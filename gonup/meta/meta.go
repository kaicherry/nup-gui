package meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/kaicherry/nup/format"
)

func ParseBlock(block []byte) map[uint16][]byte {
	out := map[uint16][]byte{}
	pos := 0
	for pos+8 <= len(block) {
		t := binary.LittleEndian.Uint16(block[pos : pos+2])
		// _r := binary.LittleEndian.Uint16(block[pos+2 : pos+4])
		size := binary.LittleEndian.Uint32(block[pos+4 : pos+8])
		if pos+8+int(size) > len(block) {
			panic("meta TLV overruns block")
		}
		if _, ok := out[t]; !ok {
			out[t] = append([]byte(nil), block[pos+8:pos+8+int(size)]...)
		}
		pos += 8 + int(size)
	}
	return out
}

func SerializeBlock(records map[uint16][]byte) []byte {
	var keys []int
	for k := range records {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	var buf bytes.Buffer
	for _, k := range keys {
		t := uint16(k)
		payload := records[t]
		var hdr [8]byte
		binary.LittleEndian.PutUint16(hdr[0:2], t)
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(payload)))
		buf.Write(hdr[:])
		buf.Write(payload)
	}
	return buf.Bytes()
}

func DecodeLocalized(payload []byte) map[string][]byte {
	out := map[string][]byte{}
	pos := 0
	for pos < len(payload) {
		if pos >= len(payload) {
			break
		}
		langLen := int(payload[pos])
		pos++
		if pos+langLen > len(payload) {
			break
		}
		lang := string(bytes.ToLower(payload[pos : pos+langLen]))
		pos += langLen
		if pos+4 > len(payload) {
			break
		}
		dataLen := int(binary.LittleEndian.Uint32(payload[pos : pos+4]))
		pos += 4
		if pos+dataLen > len(payload) {
			panic("localized record overruns TLV")
		}
		out[lang] = append([]byte(nil), payload[pos:pos+dataLen]...)
		pos += dataLen
	}
	return out
}

func EncodeLocalized(entries map[string][]byte) []byte {
	var keys []string
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	for _, lang := range keys {
		tag := strings.ToLower(lang)
		if len(tag) == 0 || len(tag) > 255 {
			panic("bad language tag")
		}
		buf.WriteByte(byte(len(tag)))
		buf.WriteString(tag)
		data := entries[lang]
		var dlen [4]byte
		binary.LittleEndian.PutUint32(dlen[:], uint32(len(data)))
		buf.Write(dlen[:])
		buf.Write(data)
	}
	return buf.Bytes()
}

func ReadFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func MergeAppMeta(existing map[uint16][]byte, name, publisher, displayVersion, icon *string, langNames, langIcons map[string]string, clearName, clearPub, clearDisp, clearIcon bool) map[uint16][]byte {
	out := map[uint16][]byte{}
	for k, v := range existing {
		out[k] = append([]byte(nil), v...)
	}
	if clearName {
		delete(out, format.TLVTitleName)
	} else if name != nil {
		out[format.TLVTitleName] = []byte(*name)
	}
	if clearPub {
		delete(out, format.TLVPublisher)
	} else if publisher != nil {
		out[format.TLVPublisher] = []byte(*publisher)
	}
	if clearDisp {
		delete(out, format.TLVDisplayVersion)
	} else if displayVersion != nil {
		out[format.TLVDisplayVersion] = []byte(*displayVersion)
	}
	if clearIcon {
		delete(out, format.TLVIcon)
	} else if icon != nil {
		out[format.TLVIcon] = ReadFile(*icon)
	}
	// localized titles
	if len(langNames) > 0 {
		loc := map[string][]byte{}
		if v, ok := out[format.TLVLocalizedTitles]; ok {
			for k, vv := range DecodeLocalized(v) {
				loc[k] = vv
			}
		}
		for lang, val := range langNames {
			if val == "" {
				delete(loc, strings.ToLower(lang))
			} else {
				loc[strings.ToLower(lang)] = []byte(val)
			}
		}
		if len(loc) == 0 {
			delete(out, format.TLVLocalizedTitles)
		} else {
			out[format.TLVLocalizedTitles] = EncodeLocalized(loc)
		}
	}
	if len(langIcons) > 0 {
		loc := map[string][]byte{}
		if v, ok := out[format.TLVLocalizedIcons]; ok {
			for k, vv := range DecodeLocalized(v) {
				loc[k] = vv
			}
		}
		for lang, val := range langIcons {
			if val == "" {
				delete(loc, strings.ToLower(lang))
			} else {
				loc[strings.ToLower(lang)] = ReadFile(val)
			}
		}
		if len(loc) == 0 {
			delete(out, format.TLVLocalizedIcons)
		} else {
			out[format.TLVLocalizedIcons] = EncodeLocalized(loc)
		}
	}
	return out
}

type MetaSource interface {
	GetNPDM() []byte
	GetBuildID() []byte
}

func MetaBlock(npdm []byte, buildID []byte, dumpLabel string, appExtra []byte) []byte {
	type rec struct {
		t uint16
		p []byte
	}
	var recs []rec
	if len(npdm) > 0 {
		recs = append(recs, rec{format.TLVNPDM, append([]byte(nil), npdm...)})
	}
	if len(buildID) > 0 {
		recs = append(recs, rec{format.TLVBuildID, append([]byte(nil), buildID...)})
	}
	recs = append(recs, rec{format.TLVSourceDump, []byte(dumpLabel)})
	// merge appExtra
	if len(appExtra) > 0 {
		pos := 0
		for pos+8 <= len(appExtra) {
			t := binary.LittleEndian.Uint16(appExtra[pos : pos+2])
			size := binary.LittleEndian.Uint32(appExtra[pos+4 : pos+8])
			if pos+8+int(size) > len(appExtra) {
				break
			}
			recs = append(recs, rec{t, append([]byte(nil), appExtra[pos+8:pos+8+int(size)]...)})
			pos += 8 + int(size)
		}
	}
	// sort
	sort.Slice(recs, func(i, j int) bool { return recs[i].t < recs[j].t })
	// dedup
	var dedup []rec
	for _, r := range recs {
		if len(dedup) > 0 && dedup[len(dedup)-1].t == r.t {
			continue
		}
		dedup = append(dedup, r)
	}
	var buf bytes.Buffer
	for _, r := range dedup {
		var hdr [8]byte
		binary.LittleEndian.PutUint16(hdr[0:2], r.t)
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(r.p)))
		buf.Write(hdr[:])
		buf.Write(r.p)
	}
	return buf.Bytes()
}

func AppMeta(names map[string]*string, icons map[string]*string, displayVersion, publisher *string, dumpLabel string) []byte {
	type rec struct {
		t uint16
		p []byte
	}
	var recs []rec
	if names != nil {
		if v, ok := names[""]; ok && v != nil {
			recs = append(recs, rec{format.TLVTitleName, []byte(*v)})
		}
		// localized
		loc := map[string][]byte{}
		for k, v := range names {
			if k == "" || v == nil {
				continue
			}
			loc[k] = []byte(*v)
		}
		if len(loc) > 0 {
			recs = append(recs, rec{format.TLVLocalizedTitles, EncodeLocalized(loc)})
		}
	}
	if publisher != nil {
		recs = append(recs, rec{format.TLVPublisher, []byte(*publisher)})
	}
	if icons != nil {
		if v, ok := icons[""]; ok && v != nil {
			recs = append(recs, rec{format.TLVIcon, ReadFile(*v)})
		}
		loc := map[string][]byte{}
		for k, v := range icons {
			if k == "" || v == nil {
				continue
			}
			loc[k] = ReadFile(*v)
		}
		if len(loc) > 0 {
			recs = append(recs, rec{format.TLVLocalizedIcons, EncodeLocalized(loc)})
		}
	}
	if displayVersion != nil {
		recs = append(recs, rec{format.TLVDisplayVersion, []byte(*displayVersion)})
	}
	recs = append(recs, rec{format.TLVSourceDump, []byte(dumpLabel)})
	sort.Slice(recs, func(i, j int) bool { return recs[i].t < recs[j].t })
	var buf bytes.Buffer
	for _, r := range recs {
		var hdr [8]byte
		binary.LittleEndian.PutUint16(hdr[0:2], r.t)
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(r.p)))
		buf.Write(hdr[:])
		buf.Write(r.p)
	}
	return buf.Bytes()
}

var ErrMeta = errors.New("meta error")
