package tunnel

import (
	"errors"
	"sync"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

type auditOutbound struct {
	C.Proxy
	tp C.AdapterType
}

func (p *auditOutbound) Type() C.AdapterType            { return p.tp }
func (*auditOutbound) SupportUDP() bool                 { return true }
func (*auditOutbound) Unwrap(*C.Metadata, bool) C.Proxy { return nil }

type auditRule struct {
	C.Rule
	host, outbound string
}

func (*auditRule) RuleType() C.RuleType { return C.Domain }
func (r *auditRule) Payload() string    { return r.host }
func (r *auditRule) Match(metadata *C.Metadata, _ C.RuleMatchHelper) (bool, string) {
	return metadata.Host == r.host, r.outbound
}

func TestZapret2RoutingReload(t *testing.T) {
	oldProxies, oldProviders, oldRules, oldSubRules, oldRuleProviders := proxies, providers, rules, subRules, ruleProviders
	oldUDP := udpRoute.Load()
	t.Cleanup(func() {
		UpdateRouting(oldProxies, oldProviders, oldRules, oldSubRules, oldRuleProviders)
		udpRoute.Store(oldUDP)
	})

	direct := &auditOutbound{tp: C.Direct}
	zapret2 := &auditOutbound{tp: C.Zapret2}
	freedom := &auditOutbound{tp: C.Freedom}
	withV2 := map[string]C.Proxy{"DIRECT": direct, "ZAPRET2": zapret2}
	withV1 := map[string]C.Proxy{"DIRECT": direct, "V1": freedom}
	ruleV2 := []C.Rule{&auditRule{host: "selected.example", outbound: "ZAPRET2"}}
	ruleV1 := []C.Rule{&auditRule{host: "selected.example", outbound: "V1"}}

	UpdateRouting(withV2, nil, ruleV2, nil, nil)
	if got, _, err := resolveMetadata(&C.Metadata{NetWork: C.UDP, Host: "direct.example"}); err != nil || got != direct {
		t.Fatalf("non-matching traffic did not select DIRECT: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			UpdateRouting(withV1, nil, ruleV1, nil, nil)
			UpdateRouting(withV2, nil, ruleV2, nil, nil)
		}
	}()
	defer wg.Wait()
	for i := 0; i < 100; i++ {
		got, _, err := resolveMetadata(&C.Metadata{NetWork: C.UDP, Host: "selected.example"})
		if errors.Is(err, errRoutingChanged) {
			continue
		}
		if err != nil || (got != zapret2 && got != freedom) {
			t.Fatalf("mixed rules and proxies during reload: %v", err)
		}
	}
}

func TestZapret2UDPSessionKeys(t *testing.T) {
	const source = "192.0.2.10:50000"
	direct := C.Metadata{NetWork: C.UDP, Host: "direct.example", DstPort: 443}
	selected := C.Metadata{NetWork: C.UDP, Host: "selected.example", DstPort: 443}
	active := &udpRouting{generation: 1, split: true}
	reloaded := &udpRouting{generation: 2, split: true}
	key := udpSessionKey(source, &selected, active)
	if key == udpSessionKey(source, &direct, active) || key == udpSessionKey(source, &selected, reloaded) {
		t.Fatal("destination or configuration generation reused the UDP socket")
	}
	if udpSessionKey(source, &direct, nil) != source {
		t.Fatal("legacy UDP session key changed")
	}
	afterRemoval := &udpRouting{generation: 3}
	if removed := udpSessionKey(source, &direct, afterRemoval); removed == source || removed == key ||
		removed != udpSessionKey(source, &selected, afterRemoval) {
		t.Fatal("removing Zapret2 must start fresh, destination-independent sessions")
	}
	selected.InUser = "another-user"
	if udpSessionKey(source, &selected, active) == key {
		t.Fatal("different inbound users must not reuse a policy-dependent UDP session")
	}
}
