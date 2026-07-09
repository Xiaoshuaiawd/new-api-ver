package common

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
	DatabaseTypePostgreSQL DatabaseType = "postgres"
	DatabaseTypeClickHouse DatabaseType = "clickhouse"
)

var mainDatabaseType = DatabaseTypeSQLite
var logDatabaseType = DatabaseTypeSQLite

var UsingSQLite = true
var UsingPostgreSQL = false
var UsingMySQL = false
var UsingClickHouse = false
var LogSqlType = DatabaseTypeSQLite
var ChatLogSqlType = DatabaseTypeSQLite

func MainDatabaseType() DatabaseType {
	return mainDatabaseType
}

func LogDatabaseType() DatabaseType {
	return logDatabaseType
}

func SetMainDatabaseType(databaseType DatabaseType) {
	mainDatabaseType = databaseType
	UsingSQLite = databaseType == DatabaseTypeSQLite
	UsingPostgreSQL = databaseType == DatabaseTypePostgreSQL
	UsingMySQL = databaseType == DatabaseTypeMySQL
}

func SetLogDatabaseType(databaseType DatabaseType) {
	logDatabaseType = databaseType
	LogSqlType = databaseType
	UsingClickHouse = databaseType == DatabaseTypeClickHouse
}

func SetDatabaseTypes(mainType DatabaseType, logType DatabaseType) {
	SetMainDatabaseType(mainType)
	SetLogDatabaseType(logType)
}

func UsingMainDatabase(databaseType DatabaseType) bool {
	return mainDatabaseType == databaseType
}

func UsingLogDatabase(databaseType DatabaseType) bool {
	return logDatabaseType == databaseType
}

var SQLitePath = "one-api.db?_busy_timeout=30000"
