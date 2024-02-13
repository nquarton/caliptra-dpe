// Licensed under the Apache-2.0 license

package verification

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"testing"

	"github.com/chipsalliance/caliptra-dpe/verification/client"
)

func TestDeriveContext(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	var resp *client.DeriveContextResp

	simulation := false
	handle := getInitialContextHandle(d, c, t, simulation)
	defer func() {
		c.DestroyContext(handle)
	}()

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}

	digestLen := profile.GetDigestSize()

	// Child context handle returned will be the default context handle when MakeDefault flag is set
	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.MakeDefault, 0, 0); err != nil {
		t.Errorf("[ERROR]: Error while creating child context handle: %s", err)
	}
	handle = &resp.NewContextHandle
	if resp.NewContextHandle != client.DefaultContextHandle {
		t.Fatalf("[FATAL]: Incorrect handle. Should return %v, but returned %v", client.DefaultContextHandle, *handle)
	}

	// When there is already a default handle, setting MakeDefault and RetainParentContext will cause error
	// because there cannot be default *and* non-default handles in a locality
	if _, err = c.DeriveContext(handle, make([]byte, digestLen), client.MakeDefault|client.RetainParentContext, 0, 0); err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}

	// Retain parent should fail because parent handle is a default handle
	// and child handle will be a non-default handle.
	// Default and non-default handle cannot coexist in same locality.
	if _, err = c.DeriveContext(handle, make([]byte, digestLen), client.RetainParentContext, 0, 0); err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}

	// Child context handle should be a random handle when MakeDefault flag is NOT used
	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), 0, 0, 0); err != nil {
		t.Errorf("[ERROR]: Error while creating child context handle: %s", err)
	}
	handle = &resp.NewContextHandle
	if resp.NewContextHandle == client.DefaultContextHandle {
		t.Fatalf("[FATAL]: Incorrect handle. Should return non-default handle, but returned %v", *handle)
	}

	// Now, there is no default handle, setting RetainParentContext flag should succeed
	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.RetainParentContext, 0, 0); err != nil {
		t.Errorf("[ERROR]: Error while making child context handle as default handle: %s", err)
	}
	handle = &resp.NewContextHandle
}

// Validates DerivedChild command with ChangeLocality flag.
func TestChangeLocality(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	if !d.HasLocalityControl() {
		t.Skip("WARNING: DPE target does not have control over locality. Skipping this test...")
	}

	var resp *client.DeriveContextResp
	simulation := false
	handle := getInitialContextHandle(d, c, t, simulation)
	// Clean up contexts
	defer func() {
		err := c.DestroyContext(handle)
		if err != nil {
			t.Errorf("[ERROR]: Error while cleaning contexts, this may cause failure in subsequent tests: %s", err)
		}
	}()

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}

	digestLen := profile.GetDigestSize()
	currentLocality := d.GetLocality()
	otherLocality := currentLocality + 1

	// Create child context in other locality
	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.ChangeLocality, 0, otherLocality); err != nil {
		t.Fatalf("[ERROR]: Error while creating child context handle in other locality: %s", err)
	}
	handle = &resp.NewContextHandle

	// Revert to same locality from other locality
	prevLocality := currentLocality
	d.SetLocality(otherLocality)

	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.ChangeLocality, 0, prevLocality); err != nil {
		t.Fatalf("[FATAL]: Error while creating child context handle in previous locality: %s", err)
	}
	handle = &resp.NewContextHandle
	d.SetLocality(prevLocality)
}

// Checks whether the DeriveContext input flags - InternalDiceInfo, InternalInputInfo are supported
// while creating child contexts when these features are supported in DPE profile.
func TestInternalInputFlags(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	var resp *client.DeriveContextResp
	simulation := false
	handle := getInitialContextHandle(d, c, t, simulation)
	defer func() {
		c.DestroyContext(handle)
	}()

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}

	digestLen := profile.GetDigestSize()
	// Internal input flags
	if d.GetSupport().InternalDice {
		if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.DeriveContextFlags(client.InternalInputDice), 0, 0); err != nil {
			t.Errorf("[ERROR]: Error while making child context handle as default handle: %s", err)
		}
		handle = &resp.NewContextHandle
	} else {
		t.Skip("[WARNING]: Profile has no support for \"InternalInputDice\" flag. Skipping the validation for this flag...")
	}

	if d.GetSupport().InternalInfo {
		if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.DeriveContextFlags(client.InternalInputInfo), 0, 0); err != nil {
			t.Errorf("[ERROR]: Error while making child context handle as default handle: %s", err)
		}
		handle = &resp.NewContextHandle
	} else {
		t.Skip("[WARNING]: Profile has no support for \"InternalInputInfo\" flag. Skipping the validation for this flag...")
	}
}

// Checks the privilege escalation of child
// When commands try to make use of features that are unsupported by child context, they fail.
func TestPrivilegesEscalation(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	var err error
	simulation := false
	handle := getInitialContextHandle(d, c, t, simulation)

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}

	digestLen := profile.GetDigestSize()

	// Create a child TCI node with no special privileges
	resp, err := c.DeriveContext(handle,
		make([]byte, digestLen),
		client.DeriveContextFlags(0),
		0, 0)
	if err != nil {
		t.Fatalf("[FATAL]: Error encountered in getting child context: %v", err)
	}
	handle = &resp.NewContextHandle

	// Adding new privileges to child that parent does NOT possess will cause failure
	_, err = c.DeriveContext(handle,
		make([]byte, digestLen),
		client.DeriveContextFlags(client.InputAllowX509|client.InputAllowCA),
		0, 0)
	if err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}

	// Similarly, when commands like CertifyKey try to make use of features/flags that are unsupported
	// by child context, it will fail.
	if _, err = c.CertifyKey(handle, make([]byte, digestLen), client.CertifyKeyX509, client.CertifyAddIsCA); err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}
}

// Checks whether the number of derived contexts (TCI nodes) are limited by MAX_TCI_NODES attribute of the profile
func TestMaxTCIs(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	var resp *client.DeriveContextResp

	simulation := false
	handle := getInitialContextHandle(d, c, t, simulation)
	defer func() { c.DestroyContext(handle) }()

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}
	digestSize := profile.GetDigestSize()

	// Get Max TCI count
	maxTciCount := int(d.GetMaxTciNodes())
	allowedTciCount := maxTciCount - 1 // since, a TCI node is already auto-initialized
	for i := 0; i < allowedTciCount; i++ {
		resp, err = c.DeriveContext(handle, make([]byte, digestSize), 0, 0, 0)
		if err != nil {
			t.Fatalf("[FATAL]: Error encountered in executing derive child: %v", err)
		}
		handle = &resp.NewContextHandle
	}

	// Exceed the Max TCI node count limit
	_, err = c.DeriveContext(handle, make([]byte, digestSize), 0, 0, 0)
	if err == nil {
		t.Fatalf("[FATAL]: Should return %q, but returned no error", client.StatusMaxTCIs)
	} else if !errors.Is(err, client.StatusMaxTCIs) {
		t.Fatalf("[FATAL]: Incorrect error type. Should return %q, but returned %q", client.StatusMaxTCIs, err)
	}
}

func TestDeriveContextSimulation(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	if !d.HasLocalityControl() {
		t.Skip("WARNING: DPE target does not have control over locality, DeriveContext in Simulation mode cannot be tested without this support. Skipping this test...")
	}
	var resp *client.DeriveContextResp

	simulation := true
	handle := getInitialContextHandle(d, c, t, simulation)
	defer func() {
		c.DestroyContext(handle)
	}()

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("Could not get profile: %v", err)
	}

	digestLen := profile.GetDigestSize()
	locality := d.GetLocality()

	// MakeDefault should fail because parent handle is a non-default handle
	// and child handle will be a default handle.
	// Default and non-default handle cannot coexist in same locality.
	if _, err = c.DeriveContext(handle, make([]byte, digestLen), client.MakeDefault, 0, 0); err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}

	// Make default child context in other locality
	childCtx, err := c.DeriveContext(handle,
		make([]byte, digestLen),
		client.DeriveContextFlags(client.ChangeLocality|client.RetainParentContext|client.MakeDefault),
		0,
		locality+1) // Switch locality to derive child context from Simulation context

	if err != nil {
		t.Fatalf("[FATAL]: Error while creating child handle: %s", err)
	}

	handle = &childCtx.NewContextHandle
	parentHandle := &childCtx.ParentContextHandle

	// Clean up parent context
	defer func() {
		err := c.DestroyContext(parentHandle)
		if err != nil {
			t.Errorf("[ERROR]: Error while cleaning contexts, this may cause failure in subsequent tests: %s", err)
		}
	}()

	d.SetLocality(locality + 1)

	defer func() {
		// Clean up contexts after test
		err := c.DestroyContext(handle)
		if err != nil {
			t.Errorf("[ERROR]: Error while cleaning up derived context, this may cause failure in subsequent tests: %s", err)
		}
		// Revert locality for other tests
		d.SetLocality(locality)
	}()

	// Retain parent should fail because parent handle is a default handle
	// and child handle will be a non-default handle.
	// Default and non-default handle cannot coexist in same locality.
	if _, err = c.DeriveContext(handle, make([]byte, digestLen), client.RetainParentContext, 0, 0); err == nil {
		t.Errorf("[ERROR]: Should return %q, but returned no error", client.StatusInvalidArgument)
	} else if !errors.Is(err, client.StatusInvalidArgument) {
		t.Errorf("[ERROR]: Incorrect error type. Should return %q, but returned %q", client.StatusInvalidArgument, err)
	}

	// Setting RetainParentContext flag should not invalidate the parent handle
	if resp, err = c.DeriveContext(handle, make([]byte, digestLen), client.RetainParentContext, 0, 0); err != nil {
		t.Fatalf("[FATAL]: Error while making child context and retaining parent handle %s", err)
	}
	handle = &resp.NewContextHandle
}

// TestDeriveContextRecursive checks whether the DeriveContext command updates the current TCI
// and cumulative TCI when the recursive flag is set.
func TestDeriveContextRecursive(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	var err error
	useSimulation := false // To indicate that simulation context is not used

	// Get default context handle
	handle := getInitialContextHandle(d, c, t, useSimulation)

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("[FATAL]: Could not get profile: %v", err)
	}
	digestLen := profile.GetDigestSize()

	tciValue := make([]byte, digestLen)
	for i := range tciValue {
		tciValue[i] = byte(i)
	}

	handle, tcbInfo, err := getTcbInfoForHandle(c, handle)
	if err != nil {
		t.Fatal(err)
	}
	lastCumulative := tcbInfo.Fwids[1].Digest

	// Set current TCI value
	_, err = c.DeriveContext(handle,
		tciValue,
		client.DeriveContextFlags(client.Recursive),
		0, 0)
	if err != nil {
		t.Fatalf("[FATAL]: Could not set TCI value: %v", err)
	}

	// Check current and cumulative measurement by CertifyKey
	expectedCumulative := computeExpectedCumulative(lastCumulative, tciValue)
	verifyMeasurements(c, t, handle, tciValue, expectedCumulative)
}

// TestDeriveContextRecursiveOnDerivedContexts tests the DeriveContext command with
// the recursive flag on derived child contexts.
func TestDeriveContextRecursiveOnDerivedContexts(d client.TestDPEInstance, c client.DPEClient, t *testing.T) {
	useSimulation := false // To indicate that simulation context is not used

	// Get default context handle
	handle := getInitialContextHandle(d, c, t, useSimulation)

	// Get digest size
	profile, err := client.GetTransportProfile(d)
	if err != nil {
		t.Fatalf("[FATAL]: Could not get profile: %v", err)
	}
	digestLen := profile.GetDigestSize()

	// Initialize TCI inputs
	tciValue := make([]byte, digestLen)
	for i := range tciValue {
		tciValue[i] = byte(i + 1)
	}

	extendTciValue := make([]byte, digestLen)
	for i := range extendTciValue {
		extendTciValue[i] = byte(i + 2)
	}

	// Preserve parent context to restore for subsequent tests.
	parentHandle, err := c.RotateContextHandle(handle, client.RotateContextHandleFlags(0))
	if err != nil {
		t.Errorf("[ERROR]: Error while rotating parent context handle, this may cause failure in subsequent tests: %s", err)
	}

	// Change parent back to default context
	defer func() {
		_, err = c.RotateContextHandle(parentHandle, client.RotateContextHandleFlags(client.TargetIsDefault))
		if err != nil {
			t.Errorf("[ERROR]: Error while restoring parent context handle as default context handle, this may cause failure in subsequent tests: %s", err)
		}
	}()

	// DeriveContext with input data, tag it and check TCI_CUMULATIVE
	childCtx, err := c.DeriveContext(parentHandle, tciValue, client.DeriveContextFlags(client.RetainParentContext|client.InputAllowX509), 0, 0)
	if err != nil {
		t.Fatalf("[FATAL]: Error while creating default child handle in default context: %s", err)
	}

	childHandle := &childCtx.NewContextHandle
	parentHandle = &childCtx.ParentContextHandle

	// Clean up contexts
	defer func() {
		err := c.DestroyContext(childHandle)
		if err != nil {
			t.Errorf("[ERROR]: Error while cleaning up derived context, this may cause failure in subsequent tests: %s", err)
		}
	}()

	childHandle, childTcbInfo, err := getTcbInfoForHandle(c, childHandle)
	if err != nil {
		t.Fatalf("[FATAL]: Could not get TcbInfo: %v", err)
	}

	if !bytes.Equal(childTcbInfo.Fwids[0].Digest, tciValue) {
		t.Errorf("[ERROR]: Got current TCI %x, expected %x", childTcbInfo.Fwids[0].Digest, tciValue)
	}

	// Check TCI_CUMULATIVE after creating child context
	wantCumulativeTCI := computeExpectedCumulative(make([]byte, digestLen), childTcbInfo.Fwids[0].Digest)
	if !bytes.Equal(childTcbInfo.Fwids[1].Digest, wantCumulativeTCI) {
		t.Errorf("[ERROR]: Child node's cumulative TCI %x, expected %x", childTcbInfo.Fwids[1].Digest, wantCumulativeTCI)
	}

	// Set current TCI value
	lastCumulative := childTcbInfo.Fwids[1].Digest
	resp, err := c.DeriveContext(childHandle,
		extendTciValue,
		client.DeriveContextFlags(client.Recursive),
		0, 0)
	if err != nil {
		t.Fatalf("[FATAL]: Could not set TCI value: %v", err)
	}
	childHandle = &resp.ParentContextHandle

	childHandle, childTcbInfo, err = getTcbInfoForHandle(c, childHandle)
	if err != nil {
		t.Fatalf("[FATAL]: Could not get TcbInfo: %v", err)
	}

	if !bytes.Equal(childTcbInfo.Fwids[0].Digest, extendTciValue) {
		t.Errorf("[ERROR]: Got current TCI %x, expected %x", childTcbInfo.Fwids[0].Digest, extendTciValue)
	}

	wantCumulativeTCI = computeExpectedCumulative(lastCumulative, extendTciValue)
	if !bytes.Equal(childTcbInfo.Fwids[1].Digest, wantCumulativeTCI) {
		t.Errorf("[ERROR]: Child node's cumulative TCI %x, expected %x", childTcbInfo.Fwids[1].Digest, wantCumulativeTCI)
	}
}

func computeExpectedCumulative(lastCumulative []byte, tciValue []byte) []byte {
	var hasher hash.Hash
	digestLen := len(lastCumulative)
	if digestLen == 32 {
		hasher = sha256.New()
	} else if digestLen == 48 {
		hasher = sha512.New384()
	}
	hasher.Write(lastCumulative)
	hasher.Write(tciValue)
	return hasher.Sum(nil)
}

func verifyMeasurements(c client.DPEClient, t *testing.T, handle *client.ContextHandle, expectedCurrent []byte, expectedCumulative []byte) {
	handle, tcbInfo, err := getTcbInfoForHandle(c, handle)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the last TcbInfo current/cumulative are as expected
	current := tcbInfo.Fwids[0].Digest
	cumulative := tcbInfo.Fwids[1].Digest
	if !bytes.Equal(current, expectedCurrent) {
		t.Errorf("[ERROR]: Unexpected TCI_CURRENT digest, want %v but got %v", expectedCurrent, current)
	}

	if !bytes.Equal(cumulative, expectedCumulative) {
		t.Errorf("[ERROR]: Unexpected cumulative TCI value, want %v but got %v", expectedCumulative, cumulative)
	}
}
