// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func (r *Responder) handleFrame(frame []byte) {
	if !r.running.Load() {
		return
	}
	pkt, ok := parseMDNSFrame(frame, r.localMAC, r.localIP, false)
	if !ok {
		return
	}
	if pkt.msg.Response {
		r.handleResponseConflict(pkt.msg)
		return
	}
	if len(pkt.msg.Authorities) > 0 {
		r.handleProbeConflict(pkt.msg.Authorities)
	}
	hostClaimed, instClaimed := r.claimedNames()
	if (!hostClaimed && !instClaimed) || len(pkt.msg.Questions) == 0 {
		return
	}
	r.queriesReceived.Add(1)

	var answers []dnsRecord
	var additionals []dnsRecord
	wantUnicast := pkt.srcPort != Port
	legacyUnicast := pkt.srcPort != Port
	ttl := r.svc.TTL
	if legacyUnicast && ttl > legacyUnicastMaxTTL {
		ttl = legacyUnicastMaxTTL
	}

	hasAnswer := make(map[string]bool)
	hasAdditional := make(map[string]bool)
	additionalKeys := make([]string, 0)
	addAns := func(key string, rr dnsRecord) {
		if !hasAnswer[key] {
			hasAnswer[key] = true
			answers = append(answers, rr)
		}
	}
	addAdd := func(key string, rr dnsRecord) {
		if !hasAnswer[key] && !hasAdditional[key] {
			hasAdditional[key] = true
			additionalKeys = append(additionalKeys, key)
			additionals = append(additionals, rr)
		}
	}

	for _, q := range pkt.msg.Questions {
		if q.Class != ClassIN && q.Class != TypeANY {
			continue
		}
		qName := strings.TrimSuffix(q.Name, ".")
		switch {
		case hostClaimed && instClaimed && strings.EqualFold(qName, r.metaServiceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("meta-ptr", r.metaPTRRecord(ttl))
		case hostClaimed && instClaimed && strings.EqualFold(qName, r.serviceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("ptr", r.ptrRecord(ttl))
			addAdd("srv", r.srvRecord(ttl))
			addAdd("txt", r.txtRecord(ttl))
			addAdd("a", r.aRecord(ttl))
		case instClaimed && strings.EqualFold(qName, r.instanceFQDN):
			matched := false
			if q.Type == TypeSRV || q.Type == TypeANY {
				addAns("srv", r.srvRecord(ttl))
				if hostClaimed {
					addAdd("a", r.aRecord(ttl))
				}
				matched = true
			}
			if q.Type == TypeTXT || q.Type == TypeANY {
				addAns("txt", r.txtRecord(ttl))
				matched = true
			}
			if matched && q.WantQU {
				wantUnicast = true
			}
		case hostClaimed && strings.EqualFold(qName, r.hostFQDN) && (q.Type == TypeA || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("a", r.aRecord(ttl))
		}
	}
	// A DNS record first selected as an Additional may be requested directly
	// by a later question in this packet. Keep its Answer occurrence and drop
	// the duplicate from Additional after processing all questions.
	filteredAdditionals := additionals[:0]
	for i, rr := range additionals {
		if !hasAnswer[additionalKeys[i]] {
			filteredAdditionals = append(filteredAdditionals, rr)
		}
	}
	additionals = filteredAdditionals

	answers = filterRecords(answers, func(rr dnsRecord) bool {
		return !knownAnswerSuppresses(rr, pkt.msg.Answers)
	})
	if len(answers) == 0 {
		return
	}
	allAdditionals := append([]dnsRecord(nil), additionals...)
	additionals = filterRecords(additionals, func(rr dnsRecord) bool {
		return !knownAnswerSuppresses(rr, pkt.msg.Answers) && additionalNeeded(rr, answers, allAdditionals)
	})

	resp := dnsMessage{
		Response:   true,
		Answers:    answers,
		Additional: additionals,
	}
	if legacyUnicast {
		resp.ID = pkt.msg.ID
		resp.Questions = append([]dnsQuestion(nil), pkt.msg.Questions...)
		for i := range resp.Answers {
			resp.Answers[i].CacheFlush = false
		}
		for i := range resp.Additional {
			resp.Additional[i].CacheFlush = false
		}
	}
	payload, err := encodeMDNSMessage(resp)
	if err != nil {
		return
	}

	dstMAC := multicastMACBytes
	dstIP := multicastIPv4Bytes
	dstPort := Port
	if wantUnicast {
		dstMAC = pkt.srcMAC
		dstIP = pkt.srcIP
		dstPort = pkt.srcPort
	}
	outFrame := buildMDNSFrame(r.localMAC, dstMAC, r.localIP, dstIP, Port, dstPort, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(outFrame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		} else if r.onError != nil {
			r.onError(err)
		}
		return
	}
	r.responsesSent.Add(1)
	if wantUnicast {
		r.unicastReplies.Add(1)
	} else {
		r.multicastReplies.Add(1)
	}
}

func filterRecords(records []dnsRecord, keep func(dnsRecord) bool) []dnsRecord {
	filtered := records[:0]
	for _, rr := range records {
		if keep(rr) {
			filtered = append(filtered, rr)
		}
	}
	return filtered
}

func knownAnswerSuppresses(rr dnsRecord, known []dnsRecord) bool {
	for _, candidate := range known {
		if candidate.Class == ClassIN && candidate.Type == rr.Type &&
			strings.EqualFold(strings.TrimSuffix(candidate.Name, "."), strings.TrimSuffix(rr.Name, ".")) &&
			bytes.Equal(recordRData(candidate), recordRData(rr)) && uint64(candidate.TTL)*2 >= uint64(rr.TTL) {
			return true
		}
	}
	return false
}

func additionalNeeded(additional dnsRecord, answers, additionals []dnsRecord) bool {
	for _, answer := range answers {
		switch answer.Type {
		case TypePTR:
			if (additional.Type == TypeSRV || additional.Type == TypeTXT) && strings.EqualFold(additional.Name, answer.PTR) {
				return true
			}
			if additional.Type == TypeA {
				for _, dependency := range additionals {
					if dependency.Type == TypeSRV && strings.EqualFold(dependency.Name, answer.PTR) && strings.EqualFold(dependency.SRVTarget, additional.Name) {
						return true
					}
				}
			}
		case TypeSRV:
			if additional.Type == TypeA && strings.EqualFold(additional.Name, answer.SRVTarget) {
				return true
			}
		}
	}
	return false
}

func probeResponseConflicts(local, remote []dnsRecord) bool {
	probedNames := make(map[string]bool, len(local))
	for _, ours := range local {
		probedNames[strings.ToLower(strings.TrimSuffix(ours.Name, "."))] = true
	}
	for _, theirs := range remote {
		if theirs.TTL == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(theirs.Name, "."))
		if !probedNames[name] {
			continue
		}
		theirsData := recordRData(theirs)
		matched := false
		for _, ours := range local {
			if strings.EqualFold(strings.TrimSuffix(ours.Name, "."), name) &&
				ours.Type == theirs.Type &&
				bytes.Equal(recordRData(ours), theirsData) {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func recordSetsConflict(local, remote []dnsRecord) bool {
	for _, ours := range local {
		for _, theirs := range remote {
			if theirs.TTL != 0 && ours.Type == theirs.Type && strings.EqualFold(strings.TrimSuffix(ours.Name, "."), strings.TrimSuffix(theirs.Name, ".")) &&
				!bytes.Equal(recordRData(ours), recordRData(theirs)) {
				return true
			}
		}
	}
	return false
}

func compareProbeRecords(local, remote []dnsRecord) (conflict, localWins bool) {
	outcomes := compareProbeRecordSets(local, remote)
	if len(outcomes) == 0 {
		return false, false
	}
	names := make([]string, 0, len(outcomes))
	for name := range outcomes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		cmp := outcomes[name]
		if cmp < 0 {
			return true, false
		}
		if cmp > 0 {
			conflict = true
		}
	}
	return conflict, conflict
}

func compareProbeRecordSets(local, remote []dnsRecord) map[string]int {
	type tieRecord struct {
		class uint16
		typ   uint16
		rdata []byte
	}
	sortTieRecords := func(recs []tieRecord) {
		slices.SortFunc(recs, func(a, b tieRecord) int {
			if a.class != b.class {
				return int(a.class) - int(b.class)
			}
			if a.typ != b.typ {
				return int(a.typ) - int(b.typ)
			}
			return bytes.Compare(a.rdata, b.rdata)
		})
	}
	compareTieRecord := func(a, b tieRecord) int {
		if a.class != b.class {
			return int(a.class) - int(b.class)
		}
		if a.typ != b.typ {
			return int(a.typ) - int(b.typ)
		}
		return bytes.Compare(a.rdata, b.rdata)
	}

	localByName := make(map[string][]tieRecord)
	for _, rr := range local {
		name := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
		localByName[name] = append(localByName[name], tieRecord{
			class: rr.Class & 0x7fff,
			typ:   rr.Type,
			rdata: recordRData(rr),
		})
	}
	remoteByName := make(map[string][]tieRecord)
	for _, rr := range remote {
		if rr.TTL == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
		if _, ok := localByName[name]; ok {
			remoteByName[name] = append(remoteByName[name], tieRecord{
				class: rr.Class & 0x7fff,
				typ:   rr.Type,
				rdata: recordRData(rr),
			})
		}
	}
	if len(remoteByName) == 0 {
		return nil
	}

	names := make([]string, 0, len(remoteByName))
	for name := range remoteByName {
		names = append(names, name)
	}
	slices.Sort(names)

	outcomes := make(map[string]int, len(names))
	for _, name := range names {
		ours, theirs := localByName[name], remoteByName[name]
		sortTieRecords(ours)
		sortTieRecords(theirs)
		limit := min(len(ours), len(theirs))
		nameCmp := 0
		for i := 0; i < limit; i++ {
			if cmp := compareTieRecord(ours[i], theirs[i]); cmp != 0 {
				nameCmp = cmp
				break
			}
		}
		if nameCmp == 0 {
			switch {
			case len(ours) < len(theirs):
				nameCmp = -1
			case len(ours) > len(theirs):
				nameCmp = 1
			}
		}
		outcomes[name] = nameCmp
	}
	return outcomes
}

func recordRData(rr dnsRecord) []byte {
	switch rr.Type {
	case TypeA:
		if rr.A.Is4() {
			a4 := rr.A.As4()
			return a4[:]
		}
	case TypeSRV:
		target, err := encodeDNSName(strings.ToLower(strings.TrimSuffix(rr.SRVTarget, ".")))
		if err == nil {
			rdata := make([]byte, 6+len(target))
			binary.BigEndian.PutUint16(rdata[0:2], rr.SRVPriority)
			binary.BigEndian.PutUint16(rdata[2:4], rr.SRVWeight)
			binary.BigEndian.PutUint16(rdata[4:6], rr.SRVPort)
			copy(rdata[6:], target)
			return rdata
		}
	case TypeTXT:
		if len(rr.TXT) == 0 {
			return []byte{0}
		}
		var rdata []byte
		for _, entry := range rr.TXT {
			if len(entry) > maxTXTOctetLen {
				return nil
			}
			rdata = append(rdata, byte(len(entry)))
			rdata = append(rdata, entry...)
		}
		return rdata
	case TypePTR:
		ptr, err := encodeDNSName(strings.ToLower(strings.TrimSuffix(rr.PTR, ".")))
		if err == nil {
			return ptr
		}
	default:
		return rr.RawRData
	}
	return nil
}
