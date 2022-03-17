// Copyright (c) 2021 Terminus, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package branch

import (
	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/bundle"
	"github.com/erda-project/erda/modules/orchestrator/dbclient"
	"github.com/erda-project/erda/modules/pkg/diceworkspace"
)

// Branch of project
type Branch struct {
	db  *dbclient.DBClient
	bdl *bundle.Bundle
}

// Option 应用实例对象配置选项
type Option func(*Branch)

// New 新建应用实例 service
func New(options ...Option) *Branch {
	r := &Branch{}
	for _, op := range options {
		op(r)
	}
	return r
}

// WithDBClient 配置 db client
func WithDBClient(db *dbclient.DBClient) Option {
	return func(r *Branch) {
		r.db = db
	}
}

// WithBundle 配置 bundle
func WithBundle(bdl *bundle.Bundle) Option {
	return func(r *Branch) {
		r.bdl = bdl
	}
}

func (b *Branch) QueryBranchRules(scopeType apistructs.ScopeType, scopeID uint64) ([]*apistructs.BranchRule, error) {
	rawBranches, err := b.db.QueryBranchRules(scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	branches := b.ToApiDatas(rawBranches)
	return branches, nil
}

func (b *Branch) ToApiDatas(rules []dbclient.BranchRule) []*apistructs.BranchRule {
	branches := make([]*apistructs.BranchRule, len(rules))
	for i, r := range rules {
		branches[i] = b.ToApiData(r)
	}
	return branches
}

func (b *Branch) ToApiData(rule dbclient.BranchRule) *apistructs.BranchRule {
	return &apistructs.BranchRule{
		ID:                int64(rule.ID),
		Rule:              rule.Rule,
		ScopeID:           rule.ScopeID,
		ScopeType:         rule.ScopeType,
		IsProtect:         rule.IsProtect,
		NeedApproval:      rule.NeedApproval,
		IsTriggerPipeline: rule.IsTriggerPipeline,
		Desc:              rule.Desc,
		Workspace:         rule.Workspace,
		ArtifactWorkspace: rule.ArtifactWorkspace,
	}
}

func (b *Branch) GetAllValidBranchWorkspaces(appID int64, userID string) ([]*apistructs.ValidBranch, error) {
	var result []*apistructs.ValidBranch

	app, err := b.bdl.GetApp(uint64(appID))
	if err != nil {
		return nil, err
	}
	rules, err := b.QueryBranchRules(apistructs.ProjectScope, app.ProjectID)
	if err != nil {
		return nil, err
	}
	appRules, err := b.QueryBranchRules(apistructs.AppScope, uint64(appID))
	if err != nil {
		return nil, err
	}
	repoStats, err := b.bdl.GetGittarStats(int64(app.ID), userID)
	if err != nil {
		return nil, err
	}
	// project rule取部署信息 app rule取保护分支
	for _, branch := range repoStats.Branches {
		branchRule := diceworkspace.GetValidBranchByGitReference(branch, rules)
		branchRule.IsProtect = diceworkspace.GetValidBranchByGitReference(branch, appRules).IsProtect
		result = append(result, branchRule)
	}

	for _, tag := range repoStats.Tags {
		branchRule := diceworkspace.GetValidBranchByGitReference(tag, rules)
		branchRule.IsProtect = diceworkspace.GetValidBranchByGitReference(tag, appRules).IsProtect
		result = append(result, branchRule)
	}

	return result, nil
}
