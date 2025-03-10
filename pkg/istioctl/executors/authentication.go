// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package executors

import (
	"context"

	pkgerrors "github.com/pkg/errors"
	"istio.io/api/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/pkg/istioctl"
	"github.com/erda-project/erda/pkg/istioctl/assembler"
)

type AuthNExecutor struct {
	BaseExecutor
}

func (exe AuthNExecutor) GetName() string {
	return "authN"
}

func (exe AuthNExecutor) onServiceCreateOrUpdate(ctx context.Context, svc *apistructs.Service) (istioctl.ExecResult, error) {
	enabled := false
	if svc.TrafficSecurity.Mode == "https" {
		enabled = true
	}
	drExist := true
	paExist := true
	drPortSettings, paPortSettings := assembler.NewPortTlsSettings(svc)
	dr, err := exe.client.NetworkingV1alpha3().DestinationRules(svc.Namespace).Get(ctx, svc.Name, v1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return istioctl.ExecSkip, pkgerrors.WithStack(err)
		}
		drExist = false
		dr = assembler.NewDestinationRule(svc)
	}
	pa, err := exe.client.SecurityV1beta1().PeerAuthentications(svc.Namespace).Get(ctx, svc.Name, v1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return istioctl.ExecSkip, pkgerrors.WithStack(err)
		}
		paExist = false
		pa = assembler.NewPeerAuthentication(svc)
	}
	if enabled {
		tls := &v1alpha3.ClientTLSSettings{
			Mode: v1alpha3.ClientTLSSettings_ISTIO_MUTUAL,
		}
		if dr.Spec.TrafficPolicy == nil {
			dr.Spec.TrafficPolicy = &v1alpha3.TrafficPolicy{
				Tls: tls,
			}
		}
		if dr.Spec.TrafficPolicy.Tls == nil {
			dr.Spec.TrafficPolicy.Tls = tls
		}
		if len(drPortSettings) > 0 {
			dr.Spec.TrafficPolicy.PortLevelSettings = drPortSettings
		}
		if len(paPortSettings) > 0 {
			pa.Spec.PortLevelMtls = paPortSettings
		}
	} else {
		if dr.Spec.TrafficPolicy != nil {
			dr.Spec.TrafficPolicy.Tls = nil
			dr.Spec.TrafficPolicy.PortLevelSettings = nil
		}
		// disable 时需要先删除 pa
		err = exe.client.SecurityV1beta1().PeerAuthentications(svc.Namespace).Delete(ctx, svc.Name, v1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return istioctl.ExecSkip, pkgerrors.WithStack(err)
		}
	}
	if !drExist {
		_, err = exe.client.NetworkingV1alpha3().DestinationRules(svc.Namespace).Create(ctx, dr, v1.CreateOptions{})
	} else {
		_, err = exe.client.NetworkingV1alpha3().DestinationRules(svc.Namespace).Update(ctx, dr, v1.UpdateOptions{})
	}
	if err != nil {
		return istioctl.ExecSkip, pkgerrors.WithStack(err)
	}
	if !enabled {
		return istioctl.ExecSuccess, nil
	}
	if !paExist {
		_, err = exe.client.SecurityV1beta1().PeerAuthentications(svc.Namespace).Create(ctx, pa, v1.CreateOptions{})
	} else {
		_, err = exe.client.SecurityV1beta1().PeerAuthentications(svc.Namespace).Update(ctx, pa, v1.UpdateOptions{})
	}
	if err != nil {
		return istioctl.ExecSkip, pkgerrors.WithStack(err)
	}

	return istioctl.ExecSuccess, nil
}

// OnServiceCreate
func (exe AuthNExecutor) OnServiceCreate(ctx context.Context, svc *apistructs.Service) (istioctl.ExecResult, error) {
	return exe.onServiceCreateOrUpdate(ctx, svc)
}

// OnServiceUpdate
func (exe AuthNExecutor) OnServiceUpdate(ctx context.Context, svc *apistructs.Service) (istioctl.ExecResult, error) {
	return exe.onServiceCreateOrUpdate(ctx, svc)
}

// OnServiceDelete
func (exe AuthNExecutor) OnServiceDelete(ctx context.Context, svc *apistructs.Service) (istioctl.ExecResult, error) {
	err := exe.client.NetworkingV1alpha3().DestinationRules(svc.Namespace).Delete(ctx, svc.Name, v1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return istioctl.ExecSkip, pkgerrors.WithStack(err)
	}
	err = exe.client.SecurityV1beta1().PeerAuthentications(svc.Namespace).Delete(ctx, svc.Name, v1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return istioctl.ExecSkip, pkgerrors.WithStack(err)
	}
	return istioctl.ExecSuccess, nil
}
