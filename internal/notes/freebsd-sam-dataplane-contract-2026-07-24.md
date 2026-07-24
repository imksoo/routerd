# FreeBSD SAM local-capture contract

This note is the test-design baseline for umbrella #960, which withdraws the
then-current #894 Linux-only product boundary only after all of the following
are implemented together.

`RemoteAddressClaim.capture.type: proxy-arp` on FreeBSD must be an atomic
address-scoped capture contract, not a best-effort ARP feature:

1. routerd validates complete FreeBSD support rather than silently accepting a
   Linux-only shape;
2. a claim owns one published ARP address on its capture interface, emits BPF
   GARP only after capture becomes active, and removes any routerd-owned local
   OS address collision before publishing;
3. a routerd-owned PF rule pair forwards only the captured `/32` between the
   capture interface and delivery tunnel; empty desired state removes all
   routerd-owned forwarding artifacts;
4. CARP master/standby status gates publication, preserving standby silence;
5. status and `routerctl doctor sam` report actual published-ARP, BPF GARP,
   PF, and cleanup state rather than a generic Linux-only diagnostic; and
6. deletion/restart removes only persisted routerd-owned artifacts and rejects
   byte-preserved foreign address, published-ARP, PF, or interface state.

The native gate is multiple isolated amd64 FreeBSD router/client VMs on one L2:
positive client delivery, standby negative behavior, CARP transition, restart,
owned cleanup, and foreign preservation are all required. The existing Linux
netns proxy-ARP/GARP transition test remains mechanism coverage only; it is not
FreeBSD acceptance evidence.

Shared issue #961 fixes the controller prerequisite: `SAMController` must pass
an empty desired forward-path set to every OS adapter. Linux first performs a
read-only `iptables -S routerd_sam_forward` ownership probe: a present chain is
reconciled to empty, standard missing-chain output or `exec.ErrNotFound` is a
no-op, and permission or parse errors fail closed. The future FreeBSD adapter
uses the same contract with `pfctl -a routerd_sam_forward -sr`; controller code
must not infer forward-state ownership from a particular claim subtype.
