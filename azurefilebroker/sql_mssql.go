package azurefilebroker

import (
	"fmt"
	"net/url"

	"code.cloudfoundry.org/goshims/ioutilshim"
	"code.cloudfoundry.org/goshims/osshim"
	"code.cloudfoundry.org/goshims/sqlshim"
	"code.cloudfoundry.org/lager"
	_ "github.com/denisenkom/go-mssqldb"
)

type mssqlVariant struct {
	sql                sqlshim.Sql
	ioutil             ioutilshim.Ioutil
	os                 osshim.Os
	dbConnectionString string
	caCert             string
	dbName             string
	logger             lager.Logger
}

func NewMSSqlVariant(logger lager.Logger, username, password, host, port, dbName, caCert string) SqlVariant {
	return NewMSSqlVariantWithShims(logger, username, password, host, port, dbName, caCert, &sqlshim.SqlShim{}, &ioutilshim.IoutilShim{}, &osshim.OsShim{})
}

func NewMSSqlVariantWithShims(logger lager.Logger, username, password, host, port, dbName, caCert string, sql sqlshim.Sql, ioutil ioutilshim.Ioutil, os osshim.Os) SqlVariant {
	query := url.Values{}
	query.Add("database", dbName)

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(username, password),
		Host:     fmt.Sprintf("%s:%s", host, port),
		RawQuery: query.Encode(),
	}

	return &mssqlVariant{
		sql:                sql,
		os:                 os,
		ioutil:             ioutil,
		dbConnectionString: u.String(),
		caCert:             caCert,
		dbName:             dbName,
		logger:             logger,
	}
}

func (c *mssqlVariant) Connect() (sqlshim.SqlDB, error) {
	logger := c.logger.Session("mssql-connection-connect")
	logger.Info("start")
	defer logger.Info("end")

	if c.caCert != "" {
		query := url.Values{}
		query.Add("encrypt", "true")
		query.Add("TrustServerCertificate", "false")
		query.Add("hostNameInCertificate", "*.database.windows.net")

		c.dbConnectionString = fmt.Sprintf("%s&%s", c.dbConnectionString, query.Encode())
	}

	sqlDB, err := c.sql.Open("mssql", c.dbConnectionString)
	return sqlDB, err
}

func (c *mssqlVariant) GetInitializeDatabaseSQL() []string {
	return []string{
		`IF NOT EXISTS (SELECT * from sys.objects WHERE name='service_instances' and type = 'U')
		BEGIN
			CREATE TABLE service_instances(
				id VARCHAR(255) PRIMARY KEY,
				organization_guid VARCHAR(255),
				space_guid VARCHAR(255),
				storage_account_name VARCHAR(255),
				value VARCHAR(4096),
				CONSTRAINT storage_account UNIQUE (organization_guid, space_guid, storage_account_name)
			)
		END`,
		`IF NOT EXISTS (SELECT * from sys.objects WHERE name='service_bindings' and type = 'U')
		BEGIN
			CREATE TABLE service_bindings(
				id VARCHAR(255) PRIMARY KEY,
				value VARCHAR(4096)
			)
		END`,
		`IF NOT EXISTS (SELECT * from sys.objects WHERE name = 'file_shares' and type = 'U')
		BEGIN
			CREATE TABLE file_shares(
				id VARCHAR(255) PRIMARY KEY,
				instance_id VARCHAR(255),
				FOREIGN KEY (instance_id) REFERENCES service_instances(id),
				file_share_name VARCHAR(255),
				value VARCHAR(4096),
				CONSTRAINT file_share UNIQUE (instance_id, file_share_name)
			)
		END`,
		`IF NOT EXISTS (SELECT * from sys.procedures WHERE name = 'GetAppLockForUpdate' and type = 'P')
		BEGIN
			EXECUTE sp_executesql N'CREATE PROCEDURE GetAppLockForUpdate
				@LockName NVARCHAR(255),
				@Timeout INT
			AS
			BEGIN
				SET @Timeout = @Timeout * 1000;
				DECLARE @rc INT = 0;
				EXEC @rc = SP_GETAPPLOCK @Resource = @LockName, @LockTimeout = @Timeout, @LockMode = "Exclusive", @LockOwner = "Session";
				SELECT "RESULT" = CASE WHEN @rc < 0 THEN 0 ELSE 1 END;
			END'
		END`,
		`IF NOT EXISTS (SELECT * from sys.procedures WHERE name = 'ReleaseAppLockForUpdate' and type = 'P')
		BEGIN
			EXECUTE sp_executesql N'CREATE PROCEDURE ReleaseAppLockForUpdate
				@LockName NVARCHAR(255)
			AS
			BEGIN
				DECLARE @rc INT = 0;
				EXEC @rc = SP_RELEASEAPPLOCK @Resource = @LockName, @LockOwner = "Session";
				SELECT "RESULT" = CASE WHEN @rc < 0 THEN 0 ELSE 1 END;
			END'
		END`,
		`IF NOT EXISTS (SELECT * from sys.procedures WHERE name = 'ReleaseAppLockForUpdate' and type = 'P')
		BEGIN
			EXECUTE sp_executesql N'CREATE PROCEDURE ReleaseAppLockForUpdate
				@LockName NVARCHAR(255)
			AS
			BEGIN
				EXEC SP_RELEASEAPPLOCK @Resource = @LockName, @LockOwner = "Session";
			END'
		END`,
	}
}

func (c *mssqlVariant) GetAppLockSQL() string {
	return "GetAppLockForUpdate @LockName = ?, @Timeout = ?"
}

func (c *mssqlVariant) GetReleaseAppLockSQL() string {
	return "ReleaseAppLockForUpdate @LockName = ?"
}
