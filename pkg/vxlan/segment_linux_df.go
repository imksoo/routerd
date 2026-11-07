// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package vxlan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// The pinned netlink.Vxlan type omits IFLA_VXLAN_DF. Keep this one native
// attribute at the owning boundary, indexed by the already verified link.
func readLinuxVXLANDF(ctx context.Context, index int) (string, error) {
	request := nl.NewNetlinkRequest(unix.RTM_GETLINK, 0)
	info := nl.NewIfInfomsg(unix.AF_UNSPEC)
	info.Index = int32(index)
	request.AddData(info)
	messages, err := executeVXLANDF(ctx, request, unix.RTM_NEWLINK)
	if err != nil {
		return "", err
	}
	return decodeLinuxVXLANDF(messages, index)
}

func setLinuxVXLANDF(ctx context.Context, index int, value string) error {
	request, err := linuxVXLANDFRequest(index, value)
	if err != nil {
		return err
	}
	_, err = executeVXLANDF(ctx, request, 0)
	return err
}

func linuxVXLANDFRequest(index int, value string) (*nl.NetlinkRequest, error) {
	if index <= 0 || int(int32(index)) != index {
		return nil, errors.New("VXLAN DF requires its exact native interface index")
	}
	var setting byte
	switch value {
	case "unset":
	case "set":
		setting = 1
	case "inherit":
		setting = 2
	default:
		return nil, fmt.Errorf("invalid VXLAN outer DF mode %q", value)
	}
	request := nl.NewNetlinkRequest(unix.RTM_NEWLINK, unix.NLM_F_ACK)
	info := nl.NewIfInfomsg(unix.AF_UNSPEC)
	info.Index = int32(index)
	request.AddData(info)
	link := nl.NewRtAttr(unix.IFLA_LINKINFO, nil)
	link.AddRtAttr(unix.IFLA_INFO_KIND, nl.ZeroTerminated("vxlan"))
	data := link.AddRtAttr(unix.IFLA_INFO_DATA, nil)
	data.AddRtAttr(unix.IFLA_VXLAN_DF, []byte{setting})
	request.AddData(link)
	return request, nil
}

func executeVXLANDF(ctx context.Context, request *nl.NetlinkRequest, response uint16) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	socket, err := nl.Subscribe(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer socket.Close()
	limit := &unix.Timeval{Sec: 2}
	if err := socket.SetSendTimeout(limit); err != nil {
		return nil, err
	}
	if err := socket.SetReceiveTimeout(limit); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { socket.Close(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	request.Sockets = map[int]*nl.SocketHandle{unix.NETLINK_ROUTE: {Socket: socket}}
	messages, err := request.Execute(unix.NETLINK_ROUTE, response)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return messages, err
}

func decodeLinuxVXLANDF(messages [][]byte, index int) (string, error) {
	for _, message := range messages {
		if len(message) < unix.SizeofIfInfomsg {
			return "", errors.New("short VXLAN native link response")
		}
		if int(nl.DeserializeIfInfomsg(message).Index) != index {
			return "", errors.New("VXLAN DF readback belongs to a different native link")
		}
		attributes, err := nl.ParseRouteAttr(message[unix.SizeofIfInfomsg:])
		if err != nil {
			return "", err
		}
		for _, attribute := range attributes {
			if attribute.Attr.Type&0x3fff != unix.IFLA_LINKINFO {
				continue
			}
			link, err := nl.ParseRouteAttr(attribute.Value)
			if err != nil {
				return "", err
			}
			kind := ""
			var data []byte
			for _, field := range link {
				switch field.Attr.Type & 0x3fff {
				case unix.IFLA_INFO_KIND:
					kind = strings.TrimRight(string(field.Value), "\x00")
				case unix.IFLA_INFO_DATA:
					data = field.Value
				}
			}
			if kind != "vxlan" {
				return "", errors.New("VXLAN DF readback has a different native link kind")
			}
			fields, err := nl.ParseRouteAttr(data)
			if err != nil {
				return "", err
			}
			for _, field := range fields {
				if field.Attr.Type&0x3fff != unix.IFLA_VXLAN_DF {
					continue
				}
				if len(field.Value) != 1 || field.Value[0] > 2 {
					return "", errors.New("invalid VXLAN native outer DF value")
				}
				return [...]string{"unset", "set", "inherit"}[field.Value[0]], nil
			}
		}
	}
	return "", errors.New("VXLAN native readback omitted its outer DF mode")
}
