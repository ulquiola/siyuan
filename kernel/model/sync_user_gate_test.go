// SiYuan - From thought to insight, with agents
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package model

import (
	"testing"

	"github.com/siyuan-note/siyuan/kernel/conf"
)

func TestCheckSyncAllowsThirdPartyProviderWithoutUser(t *testing.T) {
	prepareSyncDirectoryTest(t)
	Conf.SetUser(nil)
	Conf.Sync.Load().Enabled = true
	Conf.Sync.Load().CloudName = "main"
	for _, provider := range []int{conf.ProviderS3, conf.ProviderWebDAV, conf.ProviderLocal} {
		Conf.Sync.Load().Provider = provider
		if !checkSync(false, false, true) {
			t.Fatalf("provider [%d] should not require a cloud user", provider)
		}
	}
	Conf.Sync.Load().Provider = conf.ProviderSiYuan
	if checkSync(false, false, true) {
		t.Fatal("official cloud sync should require a cloud user")
	}
}

func TestCheckAssetDownloadAccessAllowsThirdPartyProviderWithoutUser(t *testing.T) {
	prepareSyncDirectoryTest(t)
	Conf.SetUser(nil)
	Conf.Sync.Load().Enabled = true
	for _, provider := range []int{conf.ProviderS3, conf.ProviderWebDAV, conf.ProviderLocal} {
		Conf.Sync.Load().Provider = provider
		if err := checkAssetDownloadAccess(); nil != err {
			t.Fatalf("provider [%d] should not require a cloud user: %s", provider, err)
		}
	}
	Conf.Sync.Load().Provider = conf.ProviderSiYuan
	if err := checkAssetDownloadAccess(); nil == err {
		t.Fatal("official cloud asset download should require a cloud user")
	}
}
