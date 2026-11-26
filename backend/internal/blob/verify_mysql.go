package blob

import (
	"context"
	"fmt"
)

// VerifyReadyCatalog authenticates every MySQL image catalog record, including
// images that have no current business references. Keep the SQL cursor closed
// during per-image reads to work even with one connection in the pool.
func (m *Manager) VerifyReadyCatalog(ctx context.Context) (int64, error) {
	if m.DB == nil || m.Store == nil {
		return 0, fmt.Errorf("MySQL image verification requires a database and store")
	}
	type catalogRow struct {
		key, sha string
		size     int64
	}
	var after string
	var checked int64
	for {
		rows, err := m.DB.QueryContext(ctx, "SELECT name,content_sha256,size_bytes FROM image_blob WHERE state='ready' AND name>? ORDER BY name LIMIT 128", after)
		if err != nil {
			return checked, err
		}
		batch := make([]catalogRow, 0, 128)
		for rows.Next() {
			var item catalogRow
			if err = rows.Scan(&item.key, &item.sha, &item.size); err != nil {
				break
			}
			batch = append(batch, item)
		}
		if err == nil {
			err = rows.Err()
		}
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return checked, err
		}
		for _, row := range batch {
			data, err := m.Get(ctx, row.key)
			if err != nil {
				return checked, fmt.Errorf("image catalog %q: %w", row.key, err)
			}
			if int64(len(data)) != row.size || Digest(data) != row.sha {
				return checked, fmt.Errorf("image catalog %q: %w", row.key, ErrIntegrity)
			}
			after = row.key
			checked++
		}
		if len(batch) < 128 {
			return checked, nil
		}
	}
}
