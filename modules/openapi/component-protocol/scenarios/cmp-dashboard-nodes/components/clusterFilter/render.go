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

package clusterFilter

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/erda-project/erda/apistructs"
	protocol "github.com/erda-project/erda/modules/openapi/component-protocol"
	"github.com/erda-project/erda/modules/openapi/component-protocol/scenarios/cmp-dashboard-nodes/common"
)

var (
	inputSc = StateCondition{
		Key:         "q",
		Label:       "标题",
		Placeholder: "请输入关键字查询",
		Type:        "input",
		Fixed:       true,
	}
	selectSc = StateCondition{
		Key:   "labels",
		Label: "标签",
		Type:  "select",
		Fixed: true,
	}
	props = Props{
		Delay: 1000,
	}
	ops = map[string]interface{}{
		apistructs.CMPDashboardFilterOperationKey.String(): Operation{
			Reload: true,
			Key:    "clusterFilter",
		},
	}
	state = State{
		Conditions:    []StateCondition{inputSc, selectSc},
		IsFirstFilter: false,
	}
)

// SetCtxBundle 设置bundle
func (i *ComponentFilter) SetCtxBundle(b protocol.ContextBundle) error {
	if b.Bdl == nil || b.I18nPrinter == nil {
		return common.BundleEmptyErr
	}
	logrus.Infof("inParams:%+v, identity:%+v", b.InParams, b.Identity)
	i.ctxBdl = b
	return nil
}

func (i *ComponentFilter) SetComponentValue() {
	i.Props = props
	i.Operations = ops
	i.State = state
	i.Type = "ContractiveFilter"
}

// RenderProtocol 渲染组件
func (i *ComponentFilter) RenderProtocol(c *apistructs.Component) error {
	if err := common.Transfer(i.State, &c.State); err != nil {
		return err
	}
	c.Props = i.Props
	c.Operations = i.Operations
	return nil
}

func (i *ComponentFilter) Render(ctx context.Context, c *apistructs.Component, s apistructs.ComponentProtocolScenario, event apistructs.ComponentEvent, gs *apistructs.GlobalStateData) error {
	var (
		state State
		ops   []Options
		err   error
	)
	bdl := ctx.Value(protocol.GlobalInnerKeyCtxBundle.String()).(protocol.ContextBundle)
	if err = i.SetCtxBundle(bdl); err != nil {
		return err
	}
	if err = common.Transfer(c.State, &state); err != nil {
		return err
	}
	i.State = state
	switch event.Operation {
	case apistructs.InitializeOperation:
		ops, err = i.getFilterOptions()
		if err != nil {
			return err
		}
		i.State.Conditions[1].Options = ops
	default:
		logrus.Warnf("operation [%s] not support, scenario:%v, event:%v", event.Operation, s, event)
	}
	i.SetComponentValue()
	return i.RenderProtocol(c)
}

func (i *ComponentFilter) getFilterOptions() ([]Options, error) {
	clusters, err := i.ctxBdl.Bdl.ListClusters("")
	if err != nil {
		return nil, err
	}
	var ops []Options
	for _, cluster := range clusters {
		ops = append(ops, Options{
			Label: "",
			Value: cluster.Name,
		})
	}

	return ops, nil
}

func RenderCreator() protocol.CompRender {
	return &ComponentFilter{
		CommonFilter: CommonFilter{
			Type: "ContractiveFilter",
		},
	}
}
