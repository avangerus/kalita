package config

import (
	"encoding/json"
	"flag"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                string `json:"port"`
	DemoMode            bool   `json:"demoMode"`
	DSLDir              string `json:"dslDir"`
	EnumsDir            string `json:"enumsDir"`
	QueueDepthThreshold int    `json:"queueDepthThreshold"`
	DBURL               string `json:"dbUrl"`
	AutoMigrate         bool   `json:"autoMigrate"`
	PersistenceEnabled  bool   `json:"persistenceEnabled"`
	PersistenceDir      string `json:"persistenceDir"`
	SnapshotEvery       int    `json:"snapshotEvery"`

	// Файлы (локально) и задел под S3
	BlobDriver string `json:"blobDriver"` // "local" (default) | "s3"
	FilesRoot  string `json:"filesRoot"`  // для local: папка хранения

	// S3 (на будущее: пока просто читаем конфиг, драйвер подключим позже)
	S3Region   string `json:"s3Region"`
	S3Bucket   string `json:"s3Bucket"`
	S3Prefix   string `json:"s3Prefix"`
	S3Endpoint string `json:"s3Endpoint"` // опционально (MinIO/кастом)
}

func def() Config {
	return Config{
		Port:                "8080",
		DemoMode:            false,
		DSLDir:              "dsl",
		EnumsDir:            "reference/enums",
		QueueDepthThreshold: 10,
		DBURL:               "",
		AutoMigrate:         false,
		PersistenceEnabled:  false,
		PersistenceDir:      "",
		SnapshotEvery:       50,

		BlobDriver: "local",
		FilesRoot:  "uploads",

		S3Region:   "",
		S3Bucket:   "",
		S3Prefix:   "",
		S3Endpoint: "",
	}
}

func loadJSON(path string) (Config, error) {
	c := def()
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func getenv(k, fallback string) string {
	if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
func getenvBool(k string, fallback bool) bool {
	if v, ok := os.LookupEnv(k); ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "1" || v == "true" || v == "yes" {
			return true
		}
		if v == "0" || v == "false" || v == "no" {
			return false
		}
	}
	return fallback
}

// LoadWithPath читает JSON по указанному пути, потом применяет ENV и флаги.
func LoadWithPath(jsonPath string) Config {
	cfg := def()

	// JSON (если файл существует)
	if st, err := os.Stat(jsonPath); err == nil && !st.IsDir() {
		if c2, err := loadJSON(jsonPath); err == nil {
			cfg = c2
		}
	}

	// ENV overrides
	cfg.Port = getenv("KALITA_PORT", cfg.Port)
	cfg.DemoMode = getenvBool("KALITA_DEMO_MODE", cfg.DemoMode)
	cfg.DSLDir = getenv("KALITA_DSL_DIR", cfg.DSLDir)
	cfg.EnumsDir = getenv("KALITA_ENUMS_DIR", cfg.EnumsDir)
	if v := getenv("KALITA_QUEUE_DEPTH_THRESHOLD", ""); strings.TrimSpace(v) != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.QueueDepthThreshold = n
		}
	}
	cfg.DBURL = getenv("KALITA_DB_URL", cfg.DBURL)
	cfg.AutoMigrate = getenvBool("KALITA_AUTO_MIGRATE", cfg.AutoMigrate)
	cfg.PersistenceEnabled = getenvBool("KALITA_PERSISTENCE_ENABLED", cfg.PersistenceEnabled)
	cfg.PersistenceDir = getenv("KALITA_PERSISTENCE_DIR", cfg.PersistenceDir)
	if v := getenv("KALITA_SNAPSHOT_EVERY", ""); strings.TrimSpace(v) != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SnapshotEvery = n
		}
	}

	cfg.BlobDriver = getenv("KALITA_BLOB_DRIVER", cfg.BlobDriver)
	cfg.FilesRoot = getenv("KALITA_FILES_ROOT", cfg.FilesRoot)
	cfg.S3Region = getenv("KALITA_S3_REGION", cfg.S3Region)
	cfg.S3Bucket = getenv("KALITA_S3_BUCKET", cfg.S3Bucket)
	cfg.S3Prefix = getenv("KALITA_S3_PREFIX", cfg.S3Prefix)
	cfg.S3Endpoint = getenv("KALITA_S3_ENDPOINT", cfg.S3Endpoint)

	// Flags overrides
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", jsonPath, "Path to config JSON")
	port := fs.String("port", cfg.Port, "HTTP port")
	demoMode := fs.String("demo-mode", strconv.FormatBool(cfg.DemoMode), "Run deterministic demo console scenario (true/false)")
	dsl := fs.String("dsl", cfg.DSLDir, "Path to DSL directory")
	enums := fs.String("enums", cfg.EnumsDir, "Path to enums directory")
	queueDepthThreshold := fs.String("queue-depth-threshold", strconv.Itoa(cfg.QueueDepthThreshold), "Queue depth threshold for backlog-aware coordination")
	db := fs.String("db", cfg.DBURL, "Postgres URL (empty = in-memory)")
	auto := fs.String("auto-migrate", strconv.FormatBool(cfg.AutoMigrate), "Auto-migrate add-only (true/false)")
	persistenceEnabled := fs.String("persistence-enabled", strconv.FormatBool(cfg.PersistenceEnabled), "Enable file-based persistence (true/false)")
	persistenceDir := fs.String("persistence-dir", cfg.PersistenceDir, "Persistence working directory")
	snapshotEvery := fs.String("snapshot-every", strconv.Itoa(cfg.SnapshotEvery), "Snapshot cadence in persisted events")

	blob := fs.String("blob-driver", cfg.BlobDriver, "Blob driver (local/s3)")
	files := fs.String("files-root", cfg.FilesRoot, "Local files root (if blob=local)")
	s3r := fs.String("s3-region", cfg.S3Region, "S3 region")
	s3b := fs.String("s3-bucket", cfg.S3Bucket, "S3 bucket")
	s3p := fs.String("s3-prefix", cfg.S3Prefix, "S3 key prefix")
	s3e := fs.String("s3-endpoint", cfg.S3Endpoint, "S3 custom endpoint")

	_ = fs.Parse(filterKnownArgs(os.Args[1:]))

	// Если через флаг передали другой конфиг — перечитаем
	if *configPath != jsonPath {
		return LoadWithPath(*configPath)
	}

	cfg.Port = strings.TrimSpace(*port)
	cfg.DemoMode = strings.EqualFold(strings.TrimSpace(*demoMode), "true") ||
		strings.EqualFold(strings.TrimSpace(*demoMode), "1") ||
		strings.EqualFold(strings.TrimSpace(*demoMode), "yes")
	cfg.DSLDir = strings.TrimSpace(*dsl)
	cfg.EnumsDir = strings.TrimSpace(*enums)
	if n, err := strconv.Atoi(strings.TrimSpace(*queueDepthThreshold)); err == nil {
		cfg.QueueDepthThreshold = n
	}
	cfg.DBURL = strings.TrimSpace(*db)
	cfg.AutoMigrate = strings.EqualFold(strings.TrimSpace(*auto), "true") ||
		strings.EqualFold(strings.TrimSpace(*auto), "1") ||
		strings.EqualFold(strings.TrimSpace(*auto), "yes")
	cfg.PersistenceEnabled = strings.EqualFold(strings.TrimSpace(*persistenceEnabled), "true") ||
		strings.EqualFold(strings.TrimSpace(*persistenceEnabled), "1") ||
		strings.EqualFold(strings.TrimSpace(*persistenceEnabled), "yes")
	cfg.PersistenceDir = strings.TrimSpace(*persistenceDir)
	if n, err := strconv.Atoi(strings.TrimSpace(*snapshotEvery)); err == nil {
		cfg.SnapshotEvery = n
	}

	cfg.BlobDriver = strings.TrimSpace(*blob)
	cfg.FilesRoot = strings.TrimSpace(*files)
	cfg.S3Region = strings.TrimSpace(*s3r)
	cfg.S3Bucket = strings.TrimSpace(*s3b)
	cfg.S3Prefix = strings.TrimSpace(*s3p)
	cfg.S3Endpoint = strings.TrimSpace(*s3e)

	return cfg
}

func filterKnownArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-test.") {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}
