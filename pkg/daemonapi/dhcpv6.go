// SPDX-License-Identifier: BSD-3-Clause

package daemonapi

import "time"

const CommandDecline = "decline"
const DHCPv6SupervisorOwnerHeader = "X-Routerd-Supervisor-Owner"

// DHCPv6DeclineRequest binds a native DAD failure to the worker and exact
// acquisition that was applied. A delayed report cannot revoke a renewed or
// reassigned lease at the same address.
type DHCPv6DeclineRequest struct {
	SupervisorOwner string                 `json:"supervisorOwner"`
	ServerDUID      string                 `json:"serverDUID"`
	Addresses       []DHCPv6DeclineAddress `json:"addresses"`
}

type DHCPv6DeclineAddress struct {
	Address    string    `json:"address"`
	AcquiredAt time.Time `json:"acquiredAt"`
}
