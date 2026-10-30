// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import (
	"context"
	"errors"
)

type unsupportedVRFNative struct{}

func newVRFNative() vrfNative { return unsupportedVRFNative{} }
func (unsupportedVRFNative) Scope(context.Context) (string, error) {
	return "", errors.New("native VRF lifecycle is only available on Linux")
}
func (unsupportedVRFNative) Observe(context.Context, string) (vrfLink, error) {
	return vrfLink{}, errors.New("native VRF observation is only available on Linux")
}
func (unsupportedVRFNative) Create(context.Context, vrfPlan, int, string) error {
	return errors.New("native VRF creation is only available on Linux")
}
func (unsupportedVRFNative) Cookie(context.Context, vrfLink, string) error {
	return errors.New("native VRF ownership is only available on Linux")
}
func (unsupportedVRFNative) Up(context.Context, vrfLink, string) error {
	return errors.New("native VRF activation is only available on Linux")
}
func (unsupportedVRFNative) Attach(context.Context, vrfPort, vrfLink, string) error {
	return errors.New("native VRF membership is only available on Linux")
}
func (unsupportedVRFNative) Delete(context.Context, vrfLink, string) error {
	return errors.New("native VRF withdrawal is only available on Linux")
}
