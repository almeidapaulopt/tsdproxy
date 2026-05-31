// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"fmt"
	"sync"
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
	mu                  sync.Mutex
	createOrUpdateErr   error
	deleteErr           error
	createOrUpdateCalls []vipCall
	deleteCalls         []string
}

func (m *mockVIPAPI) createOrUpdateVIPService(serviceName string, ports []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createOrUpdateCalls = append(m.createOrUpdateCalls, vipCall{
		serviceName: serviceName,
		ports:       append([]string{}, ports...),
	})
	return m.createOrUpdateErr
}

func (m *mockVIPAPI) deleteVIPService(serviceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, serviceName)
	return m.deleteErr
}

func (m *mockVIPAPI) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createOrUpdateCalls = nil
	m.deleteCalls = nil
}

func (m *mockVIPAPI) getCreateOrUpdateCalls() []vipCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]vipCall{}, m.createOrUpdateCalls...)
}

func (m *mockVIPAPI) getDeleteCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.deleteCalls...)
}

func (m *mockVIPAPI) setCreateOrUpdateErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createOrUpdateErr = err
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
	return &mockListenerFactory{
		fn: func(name string, _ tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
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

	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "svc:test", calls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443"}, calls[0].ports)
	assert.Empty(t, vip.getDeleteCalls())
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

	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "svc:test", calls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443"}, calls[0].ports)

	assert.Equal(t, "svc:test", calls[1].serviceName)
	assert.ElementsMatch(t, []string{"tcp:443", "tcp:8443"}, calls[1].ports)
	assert.Empty(t, vip.getDeleteCalls())
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
	vip.reset()

	err = ss.Release("svc:test", 443)
	require.NoError(t, err)

	assert.Empty(t, vip.getDeleteCalls(), "VIP service should NOT be deleted when ports remain")
	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "svc:test", calls[0].serviceName)
	assert.ElementsMatch(t, []string{"tcp:8443"}, calls[0].ports)
}

func TestReleaseLastPortDeletesVIPService(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()
	ss := testSS(vip, factory)
	defer ss.Close()

	_, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)

	vip.reset()

	err = ss.Release("svc:test", 443)
	require.NoError(t, err)

	assert.Empty(t, vip.getCreateOrUpdateCalls(), "no update expected when releasing last port")
	delCalls := vip.getDeleteCalls()
	require.Len(t, delCalls, 1)
	assert.Equal(t, "svc:test", delCalls[0])
}

func TestListenServiceFailureReconcilesVIP(t *testing.T) {
	t.Parallel()

	listenErr := errors.New("listen failed")
	factory := &mockListenerFactory{
		fn: func(_ string, _ tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
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
	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 1, "createOrUpdate called once before listen")
	delCalls := vip.getDeleteCalls()
	require.Len(t, delCalls, 1, "VIP service should be deleted after listen failure with no existing ports")
	assert.Equal(t, "svc:test", delCalls[0])
}

func TestListenServiceFailureWithExistingPorts(t *testing.T) {
	t.Parallel()

	callNum := 0
	factory := &mockListenerFactory{
		fn: func(_ string, _ tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
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

	vip.reset()

	// Second acquire fails on ListenService.
	_, err = ss.Acquire("svc:test", 8443, true, false)
	require.Error(t, err)

	// Reconciliation: VIP service updated with only port 443 (not deleted).
	assert.Empty(t, vip.getDeleteCalls(), "VIP service should NOT be deleted when existing ports remain")
	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 2, "create+update during acquire, then reconcile update")
	lastCall := calls[len(calls)-1]
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
	require.Len(t, vip.getCreateOrUpdateCalls(), 1, "duplicate acquire rejected before API call")
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
	assert.ElementsMatch(t, []string{"svc:alpha", "svc:beta"}, vip.getDeleteCalls())
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
	delCalls := vip.getDeleteCalls()
	require.Len(t, delCalls, 1)
	assert.Equal(t, "svc:test", delCalls[0])
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

	require.Len(t, vip.getCreateOrUpdateCalls(), 1)
	assert.ElementsMatch(t, []string{"tcp:22"}, vip.getCreateOrUpdateCalls()[0].ports)
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

	require.Len(t, vip.getCreateOrUpdateCalls(), 1)
	assert.ElementsMatch(t, []string{"tcp:80"}, vip.getCreateOrUpdateCalls()[0].ports)
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

	assert.Empty(t, vip.getCreateOrUpdateCalls())
	assert.Empty(t, vip.getDeleteCalls())
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
	vip.setCreateOrUpdateErr(nil)
	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)
}

func TestListenFailureRefcountZeroCleansUp(t *testing.T) {
	t.Parallel()

	factory := &mockListenerFactory{
		fn: func(_ string, _ tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
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
	factory.fn = func(_ string, _ tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
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

	calls := vip.getCreateOrUpdateCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "svc:alpha", calls[0].serviceName)
	assert.Equal(t, "svc:beta", calls[1].serviceName)
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

	vip.reset()

	err = ss.Release("svc:alpha", 443)
	require.NoError(t, err)

	// svc:alpha deleted (it was its only port), svc:beta unaffected.
	delCalls := vip.getDeleteCalls()
	require.Len(t, delCalls, 1)
	assert.Equal(t, "svc:alpha", delCalls[0])
	assert.Empty(t, vip.getCreateOrUpdateCalls())
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
