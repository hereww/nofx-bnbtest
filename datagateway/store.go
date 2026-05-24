package datagateway

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		path = "data/data-gateway.db"
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS market_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	exchange TEXT NOT NULL,
	market_type TEXT NOT NULL,
	symbol TEXT NOT NULL,
	base TEXT NOT NULL,
	price REAL NOT NULL,
	price_change_1h REAL NOT NULL DEFAULT 0,
	price_change_4h REAL NOT NULL DEFAULT 0,
	price_change_24h REAL NOT NULL DEFAULT 0,
	quote_volume_24h REAL NOT NULL DEFAULT 0,
	open_interest_value REAL NOT NULL DEFAULT 0,
	open_interest_delta REAL NOT NULL DEFAULT 0,
	funding_rate REAL NOT NULL DEFAULT 0,
	fetched_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_market_snapshots_symbol_time ON market_snapshots(symbol, fetched_at);
CREATE INDEX IF NOT EXISTS idx_market_snapshots_exchange_symbol ON market_snapshots(exchange, market_type, symbol, fetched_at);
CREATE TABLE IF NOT EXISTS ranking_snapshots (
	name TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	created_at DATETIME NOT NULL
);
`)
	return err
}

func (s *Store) SaveSnapshots(items []MarketSnapshot) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
INSERT INTO market_snapshots (
	exchange, market_type, symbol, base, price, price_change_1h, price_change_4h,
	price_change_24h, quote_volume_24h, open_interest_value, open_interest_delta,
	funding_rate, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		_, err = stmt.Exec(
			item.Exchange, item.MarketType, item.Symbol, item.Base, item.Price,
			item.PriceChange1h, item.PriceChange4h, item.PriceChange24h,
			item.QuoteVolume24h, item.OpenInterestValue, item.OpenInterestDelta,
			item.FundingRate, item.FetchedAt,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM market_snapshots WHERE fetched_at < ?`, time.Now().Add(-48*time.Hour)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) LatestSnapshots(maxAge time.Duration) ([]MarketSnapshot, error) {
	cutoff := time.Now().Add(-maxAge)
	rows, err := s.db.Query(`
SELECT ms.exchange, ms.market_type, ms.symbol, ms.base, ms.price, ms.price_change_1h,
       ms.price_change_4h, ms.price_change_24h, ms.quote_volume_24h,
       ms.open_interest_value, ms.open_interest_delta, ms.funding_rate, ms.fetched_at
FROM market_snapshots ms
JOIN (
	SELECT exchange, market_type, symbol, MAX(fetched_at) AS fetched_at
	FROM market_snapshots
	WHERE fetched_at >= ?
	GROUP BY exchange, market_type, symbol
) latest
ON latest.exchange = ms.exchange AND latest.market_type = ms.market_type
AND latest.symbol = ms.symbol AND latest.fetched_at = ms.fetched_at`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketSnapshot
	for rows.Next() {
		var item MarketSnapshot
		if err := rows.Scan(
			&item.Exchange, &item.MarketType, &item.Symbol, &item.Base, &item.Price,
			&item.PriceChange1h, &item.PriceChange4h, &item.PriceChange24h,
			&item.QuoteVolume24h, &item.OpenInterestValue, &item.OpenInterestDelta,
			&item.FundingRate, &item.FetchedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) HistoricalPrice(symbol string, ago time.Duration) (float64, bool) {
	row := s.db.QueryRow(`
SELECT price FROM market_snapshots
WHERE symbol = ? AND price > 0 AND fetched_at <= ?
ORDER BY fetched_at DESC LIMIT 1`, normalizeSymbol(symbol), time.Now().Add(-ago))
	var price float64
	if err := row.Scan(&price); err != nil || price <= 0 {
		return 0, false
	}
	return price, true
}

func (s *Store) SaveRanking(name string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO ranking_snapshots(name, payload, created_at)
VALUES(?, ?, ?)
ON CONFLICT(name) DO UPDATE SET payload = excluded.payload, created_at = excluded.created_at`,
		name, string(raw), time.Now())
	return err
}

func (s *Store) SnapshotCount() int {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM market_snapshots WHERE fetched_at >= ?`, time.Now().Add(-24*time.Hour))
	var count int
	_ = row.Scan(&count)
	return count
}
