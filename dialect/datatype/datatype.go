package datatype

/*
Go 数据类型与各个数据库数据类型的默认转换关系表
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| Go datatype	| MySQL					| PostgresSQL		| Sqlite3	| remark
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| int			| INT					| INTEGER			| INTEGER	| 有符号整数（在 32 字节系统为 4 字节有符号整型，64 字节系统为 8 字节有符号整型）
| int8			| TINYINT				| SMALLINT			| INTEGER	| 1 字节有符号整数（-128 ~ 127）
| int16			| SMALLINT				| SMALLINT			| INTEGER	| 2 字节有符号整型（-32768 ~ 32767）
| int32			| INT					| INTEGER			| INTEGER	| 4 字节有符号整型（-2147483648 ~ 2147483647）
| int64			| BIGINT				| INTEGER			| INTEGER	| 8 字节有符号整型（-9223372036854775808 ~ 9223372036854775807）
| uint			| INT UNSIGNED			| INTEGER			| INTEGER	| 无符号整数（在 32 字节系统为 4 字节有符号整型，64 字节系统为 8 字节有符号整型）
| uint8			| TINYINT UNSIGNED		| SMALLINT			| INTEGER	| 1 字节无符号整数（0 ~ 255）
| uint16		| SMALLINT UNSIGNED		| SMALLINT			| INTEGER	| 2 字节无符号整型（0 ~ 65535）
| uint32		| INT UNSIGNED			| INTEGER			| INTEGER	| 4 字节无符号整型（0 ~ 4294967295）
| uint64		| BIGINT UNSIGNED		| INTEGER			| INTEGER	| 8 字节无符号整型（0 ~ 18446744073709551615）
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| datatype.Rune	| CHAR(1)				| CHAR(1)			| TEXT		| 字符类型（1 字节）
| string		| VARCHAR				| VARCHAR			| TEXT		| 变长字符串类型（最大 65535 字节）
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| time.Time		| DATETIME				| TIMESTAMPTZ		| NUMERIC	| 日期时间（MySQL 为 YYYY-MM-DD HH:MM:SS，PostgreSQL 带时区）
| time.Duration	| BIGINT				| INTERVAL			| INTEGER	| 时长：MySQL/SQLite 存纳秒，PostgreSQL 存 interval
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| float32		| FLOAT					| REAL				| REAL		| 单精度浮点类型（4 字节，精度约 7 位小数）
| float64		| DOUBLE				| DOUBLE PRECISION	| REAL		| 双精度浮点类型（8 字节，精度约 15 位小数）
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| datatype.Byte	| TINYINT UNSIGNED		| BYTEA				| BLOB		| 字节类型（1 字节）
| []byte		| BLOB					| BYTEA				| BLOB		| 字节序类型（最大 65535 字节）
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
| bool			| TINYINT(1)			| BOOLEAN			| INTEGER	| 布尔类型（1 位）
| []any			| JSON					| JSONB				| TEXT		| 列表类型
| map[any]any	| JSON					| JSONB				| TEXT		| JSON 类型
+---------------+-----------------------+-------------------+-----------+----------------------------------------------------------------------------
*/

// Datatype 定义数据表字段类型的数据类型
type Datatype string

const (
	/****************** 数值类型 ******************/

	TINYINT   Datatype = "TINYINT"
	SMALLINT  Datatype = "SMALLINT"
	MEDIUMINT Datatype = "MEDIUMINT"
	INT       Datatype = "INT"
	BIGINT    Datatype = "BIGINT"
	INTEGER   Datatype = "INTEGER"

	REAL    Datatype = "REAL"
	FLOAT   Datatype = "FLOAT"
	DOUBLE  Datatype = "DOUBLE"
	DECIMAL Datatype = "DECIMAL"
	NUMERIC Datatype = "NUMERIC"

	/****************** 字符类型 ******************/

	CHAR       Datatype = "CHAR"
	VARCHAR    Datatype = "VARCHAR"
	TINYTEXT   Datatype = "TINYTEXT"
	TEXT       Datatype = "TEXT"
	MEDIUMTEXT Datatype = "MEDIUMTEXT"
	LONGTEXT   Datatype = "LONGTEXT"

	/****************** 时间类型 ******************/

	DATE       Datatype = "DATE"
	DATETIME   Datatype = "DATETIME"
	TIME       Datatype = "TIME"
	TIMESTAMP  Datatype = "TIMESTAMP"
	TIMESTAMPZ Datatype = "TIMESTAMPZ"
	YEAR       Datatype = "YEAR"

	// TIMESTAMPTZ 带时区的时间戳（PostgreSQL 推荐的时间类型）
	TIMESTAMPTZ Datatype = "TIMESTAMPTZ"
	// INTERVAL 时间间隔（PostgreSQL）
	INTERVAL Datatype = "INTERVAL"

	/****************** 字节类型 ******************/

	BIT        Datatype = "BIT"
	BINARY     Datatype = "BINARY"
	VARBINARY  Datatype = "VARBINARY"
	TINYBLOB   Datatype = "TINYBLOB"
	BLOB       Datatype = "BLOB"
	MEDIUMBLOB Datatype = "MEDIUMBLOB"
	LONGBLOB   Datatype = "LONGBLOB"
	BYTEA      Datatype = "BYTEA"

	/****************** 其他类型 ******************/

	SET       Datatype = "SET"
	ENUM      Datatype = "ENUM"
	JSON      Datatype = "JSON"
	JSONB     Datatype = "JSONB"
	BOOL      Datatype = "BOOL"
	BOOLEAN   Datatype = "BOOLEAN"
	SERIAL    Datatype = "SERIAL"
	BIGSERIAL Datatype = "BIGSERIAL"
	INET      Datatype = "INET"
)
