// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tsnet"
)

// --- mock implementations ---

type vipCall struct {
	serviceName string
	ports       []string
}

type mockVIPAPI struct {
	createOrUpdateErr   error
	deleteErr           error
	createOrUpdateCalls []vipCall
	deleteCalls         []string
}

func (m *mockVIPAPI) createOrUpdateVIPService(serviceName string, ports []string) error {
	m.createOrUpdateCalls = append(m.createOrUpdateCalls, vipCall{
		serviceName: serviceName,
		ports:       append([]string{}, ports...),
	})
	return m.createOrUpdateErr
}

func (m *mockVIPAPI) deleteVIPService(serviceName string) error {
	m.deleteCalls = append(m.deleteCalls, serviceName)
	return m.deleteErr
}

type mockListenerFactory struct {
	fn       func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error)
	calls    []string // records "name:mode"
	closeCnt int
}

func (m *mockListenerFactory) ListenService(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
	m.calls = append(m.calls, fmt.Sprintf("%s:%v", name, mode))
	return m.fn(name, mode)
}

func (m *mockListenerFactory) Close(_ *tsnet.ServiceListener) error {
	m.closeCnt++
	return nil
}

func newFakeServiceListener(fqdn string) *tsnet.ServiceListener {
	return &tsnet.ServiceListener{FQDN: fqdn}
}

func testSS(vip *mockVIPAPI, factory *mockListenerFactory) *ServicesServer {
	return NewServicesServer(ServicesServerConfig{
		Hostname:        "test-services",
		Log:             zerolog.Nop(),
		VIPServiceAPI:   vip,
		ListenerFactory: factory,
	})
}

func defaultFactory() *mockListenerFactory {
	callNum := 0
	return &mockListenerFactory{
		fn: func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
			callNum++
			return newFakeServiceListener(name + ".tailnet.ts.net"), nil
		},
	}
}

// --- tests ---

func TestAcquireSucceeds(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)

	assert.Equal(t, "svc:test.tailnet.ts.net", sl.FQDN)

	require.Len(t, vip.createOrUpdateCalls, 1)
	assert.Equal(t, "svc:test", vip.createOrUpdateCalls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443"}, vip.createOrUpdateCalls[0].ports)
	assert.Empty(t, vip.deleteCalls)
}

func TestAcquireSecondPortSameService(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)

	_, err = ss.Acquire("svc:test", 8443, true, false)
	require.NoError(t, err)

	require.Len(t, vip.createOrUpdateCalls, 2)
	assert.Equal(t, "svc:test", vip.createOrUpdateCalls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443"}, vip.createOrUpdateCalls[0].ports)

	assert.Equal(t, "svc:test", vip.createOrUpdateCalls[1].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443", "tcp:8443"}, vip.createOrUpdateCalls[1].ports)
	assert.Empty(t, vip.deleteCalls)
}

func TestReleaseOnePortOfMultiPortService(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	_, err = ss.Acquire("svc:test", 8443, true, false)
	require.NoError(t, err)

	// Reset tracking to isolate release calls.
	vip.createOrUpdateCalls = nil
	vip.deleteCalls = nil

	err = ss.Release("svc:test", 443)
	require.NoError(t, err)

	assert.Empty(t, vip.deleteCalls, "VIP service should NOT be deleted when ports remain")
	require.Len(t, vip.createOrUpdateCalls, 1)
	assert.Equal(t, "svc:test", vip.createOrUpdateCalls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:8443"}, vip.createOrUpdateCalls[0].ports)
}

func TestReleaseLastPortDeletesVIPService(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)

	vip.createOrUpdateCalls = nil
	vip.deleteCalls = nil

	err = ss.Release("svc:test", 443)
	require.NoError(t, err)

	assert.Empty(t, vip.createOrUpdateCalls, "no update expected when releasing last port")
	require.Len(t, vip.deleteCalls, 1)
	assert.Equal(t, "svc:test", vip.deleteCalls[0])
}

func TestListenServiceFailureReconcilesVIP(t *testing.T) {
	t.Parallel()

	listenErr := errors.New("listen failed")
	factory := &mockListenerFactory{
		fn: func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
			return nil, listenErr
		},
	}
	vip := &mockVIPAPI{}
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listen service")

	// VIP service was created then reconciled (deleted, since no existing ports).
	require.Len(t, vip.createOrUpdateCalls, 1, "createOrUpdate called once before listen")
	require.Len(t, vip.deleteCalls, 1, "VIP service should be deleted after listen failure with no existing ports")
	assert.Equal(t, "svc:test", vip.deleteCalls[0])
}

func TestListenServiceFailureWithExistingPorts(t *testing.T) {
	t.Parallel()

	callNum := 0
	factory := &mockListenerFactory{
		fn: func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
			callNum++
			if callNum == 1 {
				return newFakeServiceListener("svc:test.tailnet.ts.net"), nil
			}
			return nil, errors.New("listen failed")
		},
	}
	vip := &mockVIPAPI{}
	ss := testSS(vip, factory)
	defer ss.Close()

	// First acquire succeeds.
	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)

	vip.createOrUpdateCalls = nil
	vip.deleteCalls = nil

	// Second acquire fails on ListenService.
	_, err = ss.Acquire("svc:test", 8443, true, false)
	require.Error(t, err)

	// Reconciliation: VIP service updated with only port 443 (not deleted).
	assert.Empty(t, vip.deleteCalls, "VIP service should NOT be deleted when existing ports remain")
	require.Len(t, vip.createOrUpdateCalls, 2, "create+update during acquire, then reconcile update")
	// Last call should be the reconciliation with just the existing port.
	lastCall := vip.createOrUpdateCalls[len(vip.createOrUpdateCalls)-1]
	assert.Equal(t, "svc:test", lastCall.serviceName)
	assert.ElementsMatch(t, []string{"tcp:443"}, lastCall.ports)
}

func TestCreateOrUpdateVIPServiceFailure(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{
		createOrUpdateErr: errors.New("API error"),
	}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create VIP service")

	// No listener was created since VIP API failed first.
	assert.Empty(t, factory.calls)
}

func TestDuplicateAcquireSameServicePort(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	sl1, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl1)

	// Duplicate acquire of same service:port.
	sl2, err := ss.Acquire("svc:test", 443, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already acquired")
	assert.Nil(t, sl2)

	// Only one createOrUpdate call (for the first acquire).
	require.Len(t, vip.createOrUpdateCalls, 1, "duplicate acquire rejected before API call")
}

func TestCloseWhileRunning(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)

	// Acquire two services with different names.
	_, err := ss.Acquire("svc:alpha", 443, true, false)
	require.NoError(t, err)
	_, err = ss.Acquire("svc:beta", 8443, true, false)
	require.NoError(t, err)

	// Close while listeners are active.
	ss.Close()

	// Verify done channel is closed.
	select {
	case <-ss.done:
	default:
		t.Fatal("done channel should be closed after Close")
	}

	// Both VIP services should be deleted.
	assert.ElementsMatch(t, []string{"svc:alpha", "svc:beta"}, vip.deleteCalls)
}

func TestCloseWhileRunningMultiPortSameService(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	_, err = ss.Acquire("svc:test", 8443, true, false)
	require.NoError(t, err)

	ss.Close()

	select {
	case <-ss.done:
	default:
		t.Fatal("done channel should be closed after Close")
	}

	// Same service name appears once (dedup in stopRuntime).
	require.Len(t, vip.deleteCalls, 1)
	assert.Equal(t, "svc:test", vip.deleteCalls[0])
}

func TestAcquireTCPMode(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	sl, err := ss.Acquire("svc:test", 22, false, true)
	require.NoError(t, err)
	require.NotNil(t, sl)

	require.Len(t, vip.createOrUpdateCalls, 1)
	assert.ElementsMatch(t, []string{"tcp:22"}, vip.createOrUpdateCalls[0].ports)
}

func TestAcquireHTTPMode(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	sl, err := ss.Acquire("svc:test", 80, false, false)
	require.NoError(t, err)
	require.NotNil(t, sl)

	require.Len(t, vip.createOrUpdateCalls, 1)
	assert.ElementsMatch(t, []string{"tcp:80"}, vip.createOrUpdateCalls[0].ports)
}

func TestReleaseUnknownServicePort(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	// Release on a port that was never acquired — should succeed silently.
	err := ss.Release("svc:nonexistent", 443)
	require.NoError(t, err)

	assert.Empty(t, vip.createOrUpdateCalls)
	assert.Empty(t, vip.deleteCalls)
}

func TestCreateOrUpdateFailureRefcountZero(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{
		createOrUpdateErr: errors.New("API error"),
	}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.Error(t, err)

	// After failed acquire with refCount=0, runtime is cleaned up.
	// A subsequent acquire should attempt to start fresh.
	vip.createOrUpdateErr = nil
	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)
}

func TestListenFailureRefcountZeroCleansUp(t *testing.T) {
	t.Parallel()

	factory := &mockListenerFactory{
		fn: func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
			return nil, errors.New("listen failed")
		},
	}
	vip := &mockVIPAPI{}
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.Error(t, err)

	// refCount was 0, so runtime was cleaned up.
	// Verify by doing a successful acquire next.
	factory.fn = func(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
		return newFakeServiceListener("svc:test.tailnet.ts.net"), nil
	}
	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)
}

func TestAcquireMultipleServices(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:alpha", 443, true, false)
	require.NoError(t, err)
	_, err = ss.Acquire("svc:beta", 443, true, false)
	require.NoError(t, err)

	require.Len(t, vip.createOrUpdateCalls, 2)
	assert.Equal(t, "svc:alpha", vip.createOrUpdateCalls[0].serviceName)
	assert.Equal(t, "svc:beta", vip.createOrUpdateCalls[1].serviceName)
}

func TestReleaseOneOfTwoServices(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:alpha", 443, true, false)
	require.NoError(t, err)
	_, err = ss.Acquire("svc:beta", 8443, true, false)
	require.NoError(t, err)

	vip.createOrUpdateCalls = nil
	vip.deleteCalls = nil

	err = ss.Release("svc:alpha", 443)
	require.NoError(t, err)

	// svc:alpha deleted (it was its only port), svc:beta unaffected.
	require.Len(t, vip.deleteCalls, 1)
	assert.Equal(t, "svc:alpha", vip.deleteCalls[0])
	assert.Empty(t, vip.createOrUpdateCalls)
}

func TestDeleteVIPServiceErrorOnRelease(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{
		deleteErr: errors.New("delete failed"),
	}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)

	// Release should still succeed (VIP delete failure is logged, not propagated).
	err = ss.Release("svc:test", 443)
	require.NoError(t, err)
}
