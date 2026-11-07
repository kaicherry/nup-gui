package verify

import (
	"crypto/sha256"
	"errors"

	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/read"
)

func Verify(path string) (bool, []string) {
	var problems []string
	r, err := read.Open(path)
	if err != nil {
		return false, []string{"open: " + err.Error()}
	}
	defer r.Close()
	s := r.Super
	if s["toc_offset"].(uint64) != format.HeaderSize {
		problems = append(problems, "bad toc offset")
	}
	if s["file_size"].(uint64) < s["blob_offset"].(uint64) {
		problems = append(problems, "bad sizes")
	}
	// deep check
	for i := 0; i < len(r.FileNames); i++ {
		name, size, sha, _, esz, codec := r.FileInfo(i)
		if size == 0 {
			continue
		}
		data, err := r.ReadFile(i)
		if err != nil {
			problems = append(problems, "read failed")
			continue
		}
		got := sha256.Sum256(data)
		if got != sha {
			problems = append(problems, string(name)+": sha mismatch")
			continue
		}
		if codec == format.CodecRaw && esz != size {
			problems = append(problems, "raw size mismatch")
		}
	}
	if len(problems) > 0 {
		return false, problems
	}
	return true, nil
}

var ErrVerify = errors.New("verify failed")
