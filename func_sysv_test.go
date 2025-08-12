// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

//go:build darwin || freebsd || (linux && (amd64 || arm64))

package purego_test

import (
	"testing"

	"github.com/jwijenbergh/purego"
)

func TestUnrefCallback(t *testing.T) {
	imp := func() bool {
		return true
	}

	if err := purego.UnrefCallback(0); err == nil {
		t.Errorf("unref of unknown callback produced nil but wanted error")
	}

	ref := purego.NewCallback(imp)

	if err := purego.UnrefCallback(ref); err != nil {
		t.Errorf("callback unref produced %v but wanted nil", err)
	}
	if err := purego.UnrefCallback(ref); err == nil {
		t.Errorf("callback unref of already unref'd callback produced nil but wanted error")
	}
}

func TestUnrefCallbackFnPtr(t *testing.T) {
	imp := func() bool {
		return true
	}

	if err := purego.UnrefCallbackFnPtr(&imp); err == nil {
		t.Errorf("unref of unknown callback produced nil but wanted error")
	}

	ref := purego.NewCallbackFnPtr(&imp)

	if err := purego.UnrefCallbackFnPtr(&imp); err != nil {
		t.Errorf("unref produced %v but wanted nil", err)
	}
	if err := purego.UnrefCallbackFnPtr(&imp); err == nil {
		t.Errorf("unref of already unref'd callback produced nil but wanted error")
	}
	if err := purego.UnrefCallback(ref); err == nil {
		t.Errorf("unref of already unref'd callback ptr produced nil but wanted error")
	}
}
