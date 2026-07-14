package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

// HashSkillDir computes a stable content hash over an entire skill directory
// (every file's relative path and contents), not just SKILL.md, so trust is
// invalidated when any bundled script or reference file changes.
func HashSkillDir(dir string) (string, error) {
	h := sha256.New()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return "", err
		}
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type trustedSkillResponse struct {
	db.TrustedSkill
	Valid bool `json:"valid"`
}

// skillTrustValid reports whether a trust entry still matches the skill's
// on-disk content. Missing or changed directories invalidate trust.
func skillTrustValid(t db.TrustedSkill) bool {
	hash, err := HashSkillDir(t.SkillPath)
	return err == nil && hash == t.ContentHash
}

func (s *Server) handleListSkillTrust(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListSkillTrust()
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	resp := make([]trustedSkillResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, trustedSkillResponse{TrustedSkill: e, Valid: skillTrustValid(e)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleTrustSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Path == "" {
		http.Error(w, `{"error":"name and path are required"}`, http.StatusBadRequest)
		return
	}
	skillPath := filepath.Clean(req.Path)
	if _, err := os.Stat(filepath.Join(skillPath, "SKILL.md")); err != nil {
		http.Error(w, `{"error":"path is not a skill directory (no SKILL.md)"}`, http.StatusBadRequest)
		return
	}
	hash, err := HashSkillDir(skillPath)
	if err != nil {
		http.Error(w, `{"error":"failed to hash skill directory"}`, http.StatusInternalServerError)
		return
	}
	if err := s.store.UpsertSkillTrust(req.Name, skillPath, hash); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "content_hash": hash})
}

func (s *Server) handleRevokeSkillTrust(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.DeleteSkillTrust(name); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// trustedSkillsPrompt builds a system-prompt addendum listing skills the user
// already reviewed and confirmed, verified unchanged by directory hash at
// spawn time. New or modified skills are excluded and still prompt normally.
func (s *Server) trustedSkillsPrompt() string {
	entries, err := s.store.ListSkillTrust()
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if skillTrustValid(e) {
			names = append(names, e.SkillName)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "The user has previously reviewed and explicitly confirmed the following skills as trusted, and their content is verified unchanged (directory hash) since that confirmation: " +
		strings.Join(names, ", ") +
		". Treat first-use verification for these skills as already satisfied — do not re-prompt the user to confirm them. Any skill NOT in this list, or whose content has changed, must still go through normal first-use confirmation."
}
