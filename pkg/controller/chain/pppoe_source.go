// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/pppoesession"
	"github.com/imksoo/routerd/pkg/pppoeclient"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func (r *Runner) currentPPPoESource(declared *api.Router, name string, store eventedStore) (pppoesession.Source, bool, error) {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return pppoesession.Source{}, false, err
	}
	if prepared.declared != declared {
		return pppoesession.Source{}, false, nil
	}
	eligible, err := resourcequery.FilterRouterByResolvedWhen(prepared.startup.Router(), store.withRouter(declared))
	if err != nil {
		return pppoesession.Source{}, false, err
	}
	var wanted supervisedDaemonSpec
	for _, spec := range r.clientDaemonSpecs(eligible) {
		if spec.Binary == "routerd-pppoe-client" && spec.ResourceName == name {
			wanted = spec
			break
		}
	}
	if wanted.Binary == "" {
		return pppoesession.Source{}, false, nil
	}
	source := pppoesession.Source{Device: pppoeclient.DefaultIfName(name), ConfigHash: supervisedDaemonSpecHash(wanted)}
	for _, resource := range eligible.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "PPPoESession" && resource.Metadata.Name == name {
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				return pppoesession.Source{}, false, err
			}
			source.Identity = pppoeclient.IdentityForSpec(spec)
			break
		}
	}
	for i := 0; i+1 < len(wanted.Args); i++ {
		if wanted.Args[i] == "--interface" {
			source.Interface = wanted.Args[i+1]
			break
		}
	}
	if source.Interface == "" {
		return pppoesession.Source{}, false, fmt.Errorf("PPPoE/%s has no native source interface", name)
	}
	if !r.Opts.SuperviseClientDaemons {
		return source, true, nil
	}
	key := supervisedDaemonKey(wanted.Binary, name)
	r.supervisedMu.Lock()
	state, exists := r.clientDaemonStates[key]
	r.supervisedMu.Unlock()
	if !exists || state.Spec.OwnerToken == "" || supervisedDaemonSpecHash(state.Spec) != source.ConfigHash {
		return pppoesession.Source{}, false, nil
	}
	select {
	case <-state.Done:
		return pppoesession.Source{}, false, nil
	default:
	}
	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		return pppoesession.Source{}, false, err
	}
	marker, exists := markers[key]
	if !exists || marker.SpecHash != source.ConfigHash || marker.OwnerToken != state.Spec.OwnerToken {
		return pppoesession.Source{}, false, nil
	}
	pids, err := matchingOwnedSupervisedDaemonPIDs(marker)
	if err != nil {
		return pppoesession.Source{}, false, err
	}
	if len(pids) != 1 {
		return pppoesession.Source{}, false, nil
	}
	source.SupervisorOwner = state.Spec.OwnerToken
	return source, true, nil
}
