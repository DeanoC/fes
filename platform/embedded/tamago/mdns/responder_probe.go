// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func (r *Responder) probe(ctx context.Context) bool {
	initialDelay := r.initialProbeDelay
	if initialDelay <= 0 {
		initialDelay = time.Duration(rand.Int64N(int64(probeInterval)))
	}
	startAt := time.Now().Add(initialDelay)

	// RFC 6762 §8.1: initialize independent per-name probe schedules so a
	// tie-break loss on one name restarts only that name's probing while
	// preserving progress (or claim state) on other names.
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return false
	}
	r.hostProbe = probeNameState{nextAction: startAt}
	r.instanceProbe = probeNameState{nextAction: startAt}
	r.probing = false
	r.nameMu.Unlock()
	drainSignal(r.tieBreakCh)

	for {
		if ctx.Err() != nil {
			return false
		}
		now := time.Now()
		var sendHost, sendInst bool
		var nextWake time.Time

		r.nameMu.Lock()
		if r.conflicted {
			r.nameMu.Unlock()
			return false
		}
		if r.hostProbe.probing && r.hostProbe.attempts >= probeAttempts && !now.Before(r.hostProbe.nextAction) {
			r.hostProbe.probing = false
			r.hostProbe.claimed = true
		}
		if r.instanceProbe.probing && r.instanceProbe.attempts >= probeAttempts && !now.Before(r.instanceProbe.nextAction) {
			r.instanceProbe.probing = false
			r.instanceProbe.claimed = true
		}
		if r.hostProbe.claimed && r.instanceProbe.claimed {
			r.probing = false
			r.claimed = true
			r.nameMu.Unlock()
			return true
		}

		if !r.hostProbe.claimed && r.hostProbe.attempts < probeAttempts {
			if !now.Before(r.hostProbe.nextAction) {
				r.hostProbe.probing = true
				sendHost = true
			} else if nextWake.IsZero() || r.hostProbe.nextAction.Before(nextWake) {
				nextWake = r.hostProbe.nextAction
			}
		}
		if !r.instanceProbe.claimed && r.instanceProbe.attempts < probeAttempts {
			if !now.Before(r.instanceProbe.nextAction) {
				r.instanceProbe.probing = true
				sendInst = true
			} else if nextWake.IsZero() || r.instanceProbe.nextAction.Before(nextWake) {
				nextWake = r.instanceProbe.nextAction
			}
		}
		if !sendHost && !sendInst {
			if r.hostProbe.probing && !r.hostProbe.claimed && (nextWake.IsZero() || r.hostProbe.nextAction.Before(nextWake)) {
				nextWake = r.hostProbe.nextAction
			}
			if r.instanceProbe.probing && !r.instanceProbe.claimed && (nextWake.IsZero() || r.instanceProbe.nextAction.Before(nextWake)) {
				nextWake = r.instanceProbe.nextAction
			}
		}
		r.probing = r.hostProbe.probing || r.instanceProbe.probing
		r.nameMu.Unlock()

		if sendHost || sendInst {
			if err := r.sendProbeFor(sendHost, sendInst); err != nil {
				retryAt := time.Now().Add(probeInterval)
				r.nameMu.Lock()
				if sendHost && !r.hostProbe.claimed {
					r.hostProbe.probing = false
					r.hostProbe.attempts = 0
					r.hostProbe.nextAction = retryAt
				}
				if sendInst && !r.instanceProbe.claimed {
					r.instanceProbe.probing = false
					r.instanceProbe.attempts = 0
					r.instanceProbe.nextAction = retryAt
				}
				r.probing = r.hostProbe.probing || r.instanceProbe.probing
				r.nameMu.Unlock()
				if errors.Is(err, network.ErrPaused) {
					r.pausedDrops.Add(1)
					continue
				}
				if r.onError != nil {
					r.onError(err)
				}
				return false
			}
			sentAt := time.Now()
			r.nameMu.Lock()
			if sendHost && r.hostProbe.probing && !r.hostProbe.claimed {
				r.hostProbe.attempts++
				r.hostProbe.nextAction = sentAt.Add(probeInterval)
			}
			if sendInst && r.instanceProbe.probing && !r.instanceProbe.claimed {
				r.instanceProbe.attempts++
				r.instanceProbe.nextAction = sentAt.Add(probeInterval)
			}
			r.probing = r.hostProbe.probing || r.instanceProbe.probing
			r.nameMu.Unlock()
			continue
		}

		delay := time.Until(nextWake)
		if !r.waitProbeUntil(ctx, delay) {
			return false
		}
	}
}

func drainSignal(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (r *Responder) waitProbeUntil(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-r.stopCh:
			return false
		case <-r.conflictCh:
			return false
		case <-ctx.Done():
			return false
		case <-r.tieBreakCh:
			return true
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-r.tieBreakCh:
		return true
	case <-r.stopCh:
		return false
	case <-r.conflictCh:
		return false
	case <-ctx.Done():
		return false
	}
}

func (r *Responder) hostProbingLocked() bool {
	return !r.conflicted && (r.hostProbe.probing || (r.probing && !r.hostProbe.claimed && !r.instanceProbe.probing && !r.instanceProbe.claimed))
}

func (r *Responder) instanceProbingLocked() bool {
	return !r.conflicted && (r.instanceProbe.probing || (r.probing && !r.instanceProbe.claimed && !r.hostProbe.probing && !r.hostProbe.claimed))
}

func (r *Responder) hostClaimedLocked() bool {
	return !r.conflicted && (r.hostProbe.claimed || r.claimed)
}

func (r *Responder) instanceClaimedLocked() bool {
	return !r.conflicted && (r.instanceProbe.claimed || r.claimed)
}

func (r *Responder) claimedNames() (hostClaimed, instClaimed bool) {
	r.nameMu.Lock()
	defer r.nameMu.Unlock()
	return r.hostClaimedLocked(), r.instanceClaimedLocked()
}

func (r *Responder) isClaimed() bool {
	r.nameMu.Lock()
	defer r.nameMu.Unlock()
	return r.claimed && !r.conflicted
}

func (r *Responder) hostProbeRecords() []dnsRecord {
	rec := r.aRecord(r.svc.TTL)
	rec.CacheFlush = false
	return []dnsRecord{rec}
}

func (r *Responder) instanceProbeRecords() []dnsRecord {
	srv := r.srvRecord(r.svc.TTL)
	srv.CacheFlush = false
	txt := r.txtRecord(r.svc.TTL)
	txt.CacheFlush = false
	return []dnsRecord{srv, txt}
}

func (r *Responder) probeRecords() []dnsRecord {
	return append(r.hostProbeRecords(), r.instanceProbeRecords()...)
}

func (r *Responder) sendProbeFor(sendHost, sendInst bool) error {
	var questions []dnsQuestion
	var authorities []dnsRecord
	if sendHost {
		questions = append(questions, dnsQuestion{Name: r.hostFQDN, Type: TypeANY, Class: ClassIN})
		authorities = append(authorities, r.hostProbeRecords()...)
	}
	if sendInst {
		questions = append(questions, dnsQuestion{Name: r.instanceFQDN, Type: TypeANY, Class: ClassIN})
		authorities = append(authorities, r.instanceProbeRecords()...)
	}
	msg := dnsMessage{
		Questions:   questions,
		Authorities: authorities,
	}
	payload, err := encodeMDNSMessage(msg)
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	return r.port.SendRawFrame(frame)
}

func (r *Responder) reportNameConflict() {
	if r.onError != nil {
		r.onError(ErrResponderNameConflict)
	}
}

func (r *Responder) markNameConflictLocked() {
	if r.conflicted {
		return
	}
	r.conflicted = true
	r.probing = false
	r.hostProbe.probing = false
	r.instanceProbe.probing = false
	close(r.conflictCh)
}

func (r *Responder) handleProbeConflict(proposed []dnsRecord) {
	r.nameMu.Lock()
	hostProbing := r.hostProbingLocked()
	instProbing := r.instanceProbingLocked()
	r.nameMu.Unlock()
	if !hostProbing && !instProbing {
		return
	}

	var activeLocal []dnsRecord
	if hostProbing {
		activeLocal = append(activeLocal, r.hostProbeRecords()...)
	}
	if instProbing {
		activeLocal = append(activeLocal, r.instanceProbeRecords()...)
	}
	outcomes := compareProbeRecordSets(activeLocal, proposed)
	hostLost := hostProbing && outcomes[strings.ToLower(strings.TrimSuffix(r.hostFQDN, "."))] < 0
	instLost := instProbing && outcomes[strings.ToLower(strings.TrimSuffix(r.instanceFQDN, "."))] < 0
	if !hostLost && !instLost {
		return
	}

	retryAt := time.Now().Add(r.tieBreakDelay)
	signal := false
	r.nameMu.Lock()
	if !r.conflicted {
		if hostLost && r.hostProbingLocked() {
			r.hostProbe.probing = false
			r.hostProbe.attempts = 0
			r.hostProbe.nextAction = retryAt
			signal = true
		}
		if instLost && r.instanceProbingLocked() {
			r.instanceProbe.probing = false
			r.instanceProbe.attempts = 0
			r.instanceProbe.nextAction = retryAt
			signal = true
		}
		r.probing = r.hostProbe.probing || r.instanceProbe.probing
	}
	r.nameMu.Unlock()

	if signal {
		select {
		case r.tieBreakCh <- struct{}{}:
		default:
		}
	}
}

func (r *Responder) handleResponseConflict(msg dnsMessage) {
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return
	}
	hostProbing := r.hostProbingLocked()
	instProbing := r.instanceProbingLocked()
	hostClaimed := r.hostClaimedLocked()
	instClaimed := r.instanceClaimedLocked()
	if !hostProbing && !instProbing && !hostClaimed && !instClaimed {
		r.nameMu.Unlock()
		return
	}
	r.nameMu.Unlock()

	remote := make([]dnsRecord, 0, len(msg.Answers)+len(msg.Authorities)+len(msg.Additional))
	remote = append(remote, msg.Answers...)
	remote = append(remote, msg.Authorities...)
	remote = append(remote, msg.Additional...)

	var probingLocal []dnsRecord
	if hostProbing {
		probingLocal = append(probingLocal, r.hostProbeRecords()...)
	}
	if instProbing {
		probingLocal = append(probingLocal, r.instanceProbeRecords()...)
	}
	var claimedLocal []dnsRecord
	if hostClaimed {
		claimedLocal = append(claimedLocal, r.hostProbeRecords()...)
	}
	if instClaimed {
		claimedLocal = append(claimedLocal, r.instanceProbeRecords()...)
	}
	probeConflict := len(probingLocal) > 0 && probeResponseConflicts(probingLocal, remote)
	claimedConflict := len(claimedLocal) > 0 && recordSetsConflict(claimedLocal, remote)
	if !probeConflict && !claimedConflict {
		return
	}

	defend := false
	relinquish := false
	sendGoodbye := false
	defendHost := false
	defendInst := false
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return
	}
	if (r.hostProbingLocked() && probeResponseConflicts(r.hostProbeRecords(), remote)) ||
		(r.instanceProbingLocked() && probeResponseConflicts(r.instanceProbeRecords(), remote)) {
		defendHost = r.hostClaimedLocked()
		defendInst = r.instanceClaimedLocked()
		if (defendHost || defendInst) && !r.goodbyeSent {
			r.goodbyeSent = true
			sendGoodbye = true
		}
		r.markNameConflictLocked()
		relinquish = true
	} else {
		defendHost = r.hostClaimedLocked()
		defendInst = r.instanceClaimedLocked()
		var activeClaimed []dnsRecord
		if defendHost {
			activeClaimed = append(activeClaimed, r.hostProbeRecords()...)
		}
		if defendInst {
			activeClaimed = append(activeClaimed, r.instanceProbeRecords()...)
		}
		if len(activeClaimed) > 0 && recordSetsConflict(activeClaimed, remote) {
			now := time.Now()
			if !r.lastDefense.IsZero() && now.Sub(r.lastDefense) <= conflictDefendInterval {
				r.markNameConflictLocked()
				relinquish = true
				if !r.goodbyeSent {
					r.goodbyeSent = true
					sendGoodbye = true
				}
			} else {
				r.lastDefense = now
				defend = true
			}
		}
	}
	r.nameMu.Unlock()

	if defend {
		if err := r.sendClaimedRecords(defendHost, defendInst, r.svc.TTL, false); err != nil && !errors.Is(err, network.ErrPaused) && r.onError != nil {
			r.onError(err)
		}
	}
	if relinquish {
		r.reportNameConflict()
		if sendGoodbye {
			_ = r.sendClaimedRecords(defendHost, defendInst, 0, true)
		}
	}
}

func (r *Responder) sendClaimedRecords(hostClaimed, instClaimed bool, ttl uint32, goodbye bool) error {
	if hostClaimed && instClaimed {
		return r.sendAnnouncement(ttl, goodbye)
	}
	var answers []dnsRecord
	if instClaimed {
		answers = append(answers, r.srvRecord(ttl), r.txtRecord(ttl))
	}
	if hostClaimed {
		answers = append(answers, r.aRecord(ttl))
	}
	if len(answers) == 0 {
		return nil
	}
	payload, err := encodeMDNSMessage(dnsMessage{
		Response: true,
		Answers:  answers,
	})
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(frame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		}
		return err
	}
	if goodbye {
		r.goodbyes.Add(1)
	} else {
		r.announcements.Add(1)
	}
	return nil
}

func (r *Responder) sendAnnouncement(ttl uint32, goodbye bool) error {
	msg := dnsMessage{
		Response:   true,
		Answers:    []dnsRecord{r.ptrRecord(ttl)},
		Additional: []dnsRecord{r.srvRecord(ttl), r.txtRecord(ttl), r.aRecord(ttl)},
	}
	payload, err := encodeMDNSMessage(msg)
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(frame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		}
		return err
	}
	if goodbye {
		r.goodbyes.Add(1)
	} else {
		r.announcements.Add(1)
	}
	return nil
}

func (r *Responder) ptrRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.serviceFQDN,
		Type:       TypePTR,
		Class:      ClassIN,
		CacheFlush: false,
		TTL:        ttl,
		PTR:        r.instanceFQDN,
	}
}

func (r *Responder) metaPTRRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.metaServiceFQDN,
		Type:       TypePTR,
		Class:      ClassIN,
		CacheFlush: false,
		TTL:        ttl,
		PTR:        r.serviceFQDN,
	}
}

func (r *Responder) srvRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.instanceFQDN,
		Type:       TypeSRV,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		SRVPort:    r.svc.Port,
		SRVTarget:  r.hostFQDN,
	}
}

func (r *Responder) txtRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.instanceFQDN,
		Type:       TypeTXT,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		TXT:        r.svc.TXT,
	}
}

func (r *Responder) aRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.hostFQDN,
		Type:       TypeA,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		A:          r.svc.IP,
	}
}
