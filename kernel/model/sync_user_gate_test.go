// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

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
