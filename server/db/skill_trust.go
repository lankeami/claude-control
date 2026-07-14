package db

import (
	"fmt"
	"time"
)

type TrustedSkill struct {
	SkillName   string    `json:"skill_name"`
	SkillPath   string    `json:"skill_path"`
	ContentHash string    `json:"content_hash"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// UpsertSkillTrust records that the user confirmed a skill at a given content
// hash. Re-trusting an existing skill updates the hash and timestamp.
func (s *Store) UpsertSkillTrust(name, path, hash string) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_trust (skill_name, skill_path, content_hash, confirmed_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(skill_name) DO UPDATE SET skill_path = excluded.skill_path, content_hash = excluded.content_hash, confirmed_at = datetime('now')
	`, name, path, hash)
	if err != nil {
		return fmt.Errorf("upsert skill trust: %w", err)
	}
	return nil
}

func (s *Store) ListSkillTrust() ([]TrustedSkill, error) {
	rows, err := s.db.Query(`SELECT skill_name, skill_path, content_hash, confirmed_at FROM skill_trust ORDER BY skill_name`)
	if err != nil {
		return nil, fmt.Errorf("list skill trust: %w", err)
	}
	defer rows.Close()
	var skills []TrustedSkill
	for rows.Next() {
		var t TrustedSkill
		if err := rows.Scan(&t.SkillName, &t.SkillPath, &t.ContentHash, &t.ConfirmedAt); err != nil {
			return nil, fmt.Errorf("scan skill trust: %w", err)
		}
		skills = append(skills, t)
	}
	return skills, rows.Err()
}

func (s *Store) DeleteSkillTrust(name string) error {
	_, err := s.db.Exec(`DELETE FROM skill_trust WHERE skill_name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete skill trust: %w", err)
	}
	return nil
}
