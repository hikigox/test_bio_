// backend/cmd/api/main.go
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"energy-management/internal/api"
	"energy-management/internal/auth"
	"energy-management/internal/store"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dbPath := getenv("DB_PATH", "./energy.db")
	dataDir := getenv("DATA_DIR", "./data")
	port := getenv("PORT", "8080")
	jwtSecret := getenv("JWT_SECRET", "change-me")
	demoEmail := getenv("DEMO_EMAIL", "demo@energy.local")
	demoPassword := getenv("DEMO_PASSWORD", "demo1234")

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("abriendo BD: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatalf("migrando BD: %v", err)
	}

	seedIfEmpty(db, dataDir, demoEmail, demoPassword)

	router := api.NewRouter(&api.Server{DB: db, JWTSecret: jwtSecret})
	log.Printf("escuchando en :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

// seedIfEmpty carga readings.csv/events.csv y crea el usuario demo solo si
// la tabla readings está vacía (arranque idempotente, spec 06).
func seedIfEmpty(db *store.DB, dataDir, demoEmail, demoPassword string) {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&count)
	if count == 0 {
		if _, err := store.LoadReadingsCSV(db, dataDir+"/readings.csv"); err != nil {
			log.Printf("advertencia: no se pudo cargar readings.csv: %v", err)
		}
		if _, err := store.LoadEventsCSV(db, dataDir+"/events.csv"); err != nil {
			log.Printf("advertencia: no se pudo cargar events.csv: %v", err)
		}
		seedMetersFromReadings(db)
	}

	var userCount int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	if userCount == 0 {
		hash, _ := auth.HashPassword(demoPassword)
		db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, demoEmail, hash)
	}
}

func seedMetersFromReadings(db *store.DB) {
	rows, err := db.Query(`SELECT DISTINCT meter_id FROM readings`)
	if err != nil {
		return
	}
	defer rows.Close()

	// Buffer meter IDs before executing INSERTs to avoid deadlock on single connection
	var meterIDs []string
	for rows.Next() {
		var meterID string
		rows.Scan(&meterID)
		meterIDs = append(meterIDs, meterID)
	}

	for _, meterID := range meterIDs {
		db.Exec(`INSERT OR IGNORE INTO meters (meter_id, status, created_at) VALUES (?, 'UNKNOWN', datetime('now'))`, meterID)
	}
}

var _ = sql.ErrNoRows // evita import no usado si se recorta código en el futuro
