package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kelseyhightower/envconfig"
	"github.com/metoro-io/metoro/mrs-hudson/scraper/api"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"time"
)

const schemaName = "mrs_hudson"

type Config struct {
	Host     string `envconfig:"POSTGRES_HOST"`
	Port     string `envconfig:"POSTGRES_PORT"`
	User     string `envconfig:"POSTGRES_USER"`
	Password string `envconfig:"POSTGRES_PASSWORD"`
	Database string `envconfig:"POSTGRES_DATABASE"`
}

func getConfigFromEnvironment() (Config, error) {
	var config Config
	err := envconfig.Process("STATUSPHERE", &config)
	return config, err
}

type DbClient struct {
	db     *gorm.DB
	logger *zap.Logger
}

func NewDbClientFromEnvironment(lg *zap.Logger) (*DbClient, error) {
	config, err := getConfigFromEnvironment()
	if err != nil {
		return nil, err
	}

	// Check to see if the database exists in postgres
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable",
		config.Host, config.Port, config.User, config.Password)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	// Create the database if it does not exist
	err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", config.Database)).Error
	wasCreatedSuccessfully := false
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "42P04" {
				// This is the code for database already exists
				// We can ignore this error
				wasCreatedSuccessfully = true
			}
		}
		if !wasCreatedSuccessfully {
			return nil, errors.Wrap(err, "failed to create postgres database")
		}
	}

	// Connect to the database
	dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.Host, config.Port, config.User, config.Password, config.Database)
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second,   // Slow SQL threshold
			LogLevel:                  logger.Silent, // Log level
			IgnoreRecordNotFoundError: true,          // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      true,          // Don't include params in the SQL log
			Colorful:                  false,         // Disable color
		},
	)
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}

	return &DbClient{db: db, logger: lg}, nil
}

const statusPageTableName = "status_page"
const incidentsTableName = "incidents"

func (d *DbClient) AutoMigrate(ctx context.Context) error {
	// Create the schema if it does not exist
	d.db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName))

	// Create the statuspage table
	err := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, statusPageTableName))).AutoMigrate(&api.StatusPage{})
	if err != nil {
		return errors.Wrap(err, "failed to auto-migrate status_page table")
	}

	// Create the incidents table
	err = d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, incidentsTableName))).AutoMigrate(&api.Incident{})
	if err != nil {
		return errors.Wrap(err, "failed to auto-migrate incidents table")
	}

	return nil
}

func (d *DbClient) GetAllStatusPages(ctx context.Context) ([]api.StatusPage, error) {
	var statusPages []api.StatusPage
	result := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, statusPageTableName))).Find(&statusPages)
	if result.Error != nil {
		return nil, result.Error
	}
	return statusPages, nil
}

func (d *DbClient) GetStatusPage(ctx context.Context, url string) (*api.StatusPage, error) {
	var statusPage api.StatusPage
	result := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, statusPageTableName))).Where("url = ?", url).First(&statusPage)
	if result.Error != nil {
		return nil, result.Error
	}
	return &statusPage, nil
}

func (d *DbClient) UpdateStatusPage(ctx context.Context, statusPage api.StatusPage) error {
	result := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, statusPageTableName))).Where("url = ?", statusPage.URL).Updates(&statusPage)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (d *DbClient) InsertStatusPage(ctx context.Context, statusPage api.StatusPage) error {
	result := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, statusPageTableName))).Create(&statusPage)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (d *DbClient) GetIncidents(ctx context.Context, statusPageUrl string) ([]api.Incident, error) {
	var incidents []api.Incident
	result := d.db.Table(fmt.Sprintf(fmt.Sprintf("%s.%s", schemaName, incidentsTableName))).Where("status_page_url = ?", statusPageUrl).Find(&incidents)
	if result.Error != nil {
		return nil, result.Error
	}
	return incidents, nil
}

func (d *DbClient) CreateOrUpdateIncidents(ctx context.Context, incidents []api.Incident) error {
	result := d.db.Table(fmt.Sprintf("%s.%s", schemaName, incidentsTableName)).Clauses(
		clause.OnConflict{
			Columns:   []clause.Column{{Name: "deep_link"}},                                                                                                      // Primary key
			DoUpdates: clause.AssignmentColumns([]string{"title", "components", "events", "start_time", "end_time", "description", "impact", "status_page_url"}), // Update the data column
		},
	).Create(&incidents)
	if result.Error != nil {
		return result.Error
	}
	return nil
}
