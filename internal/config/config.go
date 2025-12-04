package config

import (
	"fmt"
	"os"
)

type Config struct {
	DBSource string
	Port     string
	Env      string
}

func Load() (*Config, error) {
	dbSource := os.Getenv("DB_SOURCE")
	if dbSource == "" {
		return nil, fmt.Errorf("DB_SOURCE environment variable is required")
	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	return &Config{
		DBSource: dbSource,
		Port:     port,
		Env:      env,
	}, nil
}
