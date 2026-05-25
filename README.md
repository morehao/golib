# golib

`golib` is a golang utility library containing common tool functions and components summarized from personal project development experience.

Components:
- [biz](#biz) Business components
- [codegen](#codegen) Code generation tools
- [concurrency](#concurrency) Concurrency control components (includes concpool, concqueue, concsem)
- [configkv](#configkv) Configuration management component
- [dbaccess](#dbaccess) Database client components (supports MySQL, Redis, Elasticsearch)
- [distlock](#distlock) Distributed lock component (non-reentrant)
- [excel](#excel) Excel read/write component
- [gast](#gast) AST syntax tree tool
- [gauth](#gauth) Authentication component (includes jwtauth)
- [gcrypto](#gcrypto) Encryption/decryption component
- [gerror](#gerror) Error handling component
- [glog](#glog) Logging component
- [gtrace](#gtrace) OpenTelemetry Trace initialization component
- [gtree](#gtree) Tree structure construction tool
- [gutil](#gutil) Common utility functions collection
- [protocol](#protocol) Protocol components (includes ghttp, gresty)
- [ratelimit](#ratelimit) Rate limiting component
- [storage](#storage) Unified object storage component (supports S3, MinIO, OSS, COS, TOS)

# Installation
```bash
go get github.com/morehao/golib
```

# Components

## biz

### Overview
`biz` is a business component package providing commonly used infrastructure components for business development.

### Sub-components
- **gcontext**: Context utilities, including request ID, user ID, tenant ID and other context key-value definitions and formatting
- **gobject**: Common business objects, including user authentication info (UserClaims), operator info (OperatorBaseInfo), pagination query (PageQuery)
- **gconstant**: Business constant definitions, including error codes (100000 series), API versions, etc.
- **gserver**: Gin server related, including route grouping and middleware integration
- **gmiddleware**: Gin middleware, including JWT authentication, CORS, access logging, Token blacklist
- **gormplugin**: GORM plugins, including multi-tenant plugin (automatically adds tenant_id filter conditions)
- **genericdao**: Generic DAO,封装基础的增删改查操作
- **testkit**: Testing toolkit, supporting test initializer and context building

### Features
- Business scenario-oriented, ready to use
- Unified error code specification
- Integrated JWT authentication and multi-tenant support

## codegen

### Overview
`codegen` is a code generation tool that reads database table structures and supports generating basic CRUD code, including router, controller, service, dto, model, errorCode, etc.

### Features
- Supports MySQL database
- Supports PostgreSQL database
- Supports template customization and template parameter customization
- Supports code generation based on templates

### Usage
For usage examples, refer to [codegen unit tests](codegen/gen_test.go)

## concurrency

### Overview
`concurrency` is a concurrency control component collection providing solutions for various concurrency scenarios.

### Sub-components
- **concpool**: Worker pool, supports task submission, concurrency control, graceful shutdown and other features
- **concqueue**: Concurrent task queue based on producer-consumer model, supports concurrency control and error statistics
- **concsem**: Semaphore control, used to limit concurrent numbers

### Features
- Flexible concurrency control
- Task queue management
- Graceful shutdown and error collection
- Thread-safe

### Usage
For usage examples, refer to [concqueue usage](concurrency/concqueue/README.md)

## configkv

### Overview
`configkv` is a configuration management component based on database key-value storage, supporting multiple data types and encryption.

### Features
- Supports json/toml/yaml/string/int/bool/float types
- Supports encrypted storage
- Based on GORM

## dbaccess

### Overview
`dbaccess` is a database client component collection providing encapsulation and connection management for multiple databases.

### Sub-components
- **dbgorm**: MySQL/PostgreSQL database client, based on GORM
- **dbredis**: Redis client, based on go-redis
- **dbes**: Elasticsearch client, based on official client

### Features
- Unified configuration interface
- Integrated logging
- Connection pool configuration support
- Timeout control support

### Usage
For usage examples, refer to [dbaccess usage](dbaccess/README.md)

## distlock

### Overview
`distlock` is a distributed lock component based on Redis, using redsync algorithm, supporting automatic renewal.

### Features
- Redis-based distributed lock
- Automatic renewal (lock keepalive)
- Non-reentrant

## excel

### Overview
`excel` is a simple wrapper around `excelize`, supporting convenient Excel file read/write through structs.

Both reading and writing Excel require defining a struct, with struct fields specifying Excel-related information through tags (`ex`).

### Features
- Define Excel column mapping through struct tags
- Support reading and writing Excel files
- Support data validation based on validator

### Usage
For usage examples, refer to [excel usage](excel/README.md)

## gast

### Overview
`gast` is a Go AST syntax tree operation tool, supporting AST analysis and code generation.

### Features
- Support function/method lookup
- Support interface method addition
- Support constant addition
- Syntax tree traversal and manipulation

## gauth

### Overview
`gauth` is an authentication component containing JWT authentication capabilities.

### Sub-components
- **jwtauth**: Generic JWT signing and parsing, supports HS256 algorithm, supports renewal

### Features
- Generic JWT signing and parsing
- Token renewal support
- Token blacklist support

### Usage
For usage examples, refer to [jwtauth usage](gauth/jwtauth/README.md)

## gcrypto

### Overview
`gcrypto` is an encryption/decryption component providing common symmetric and asymmetric encryption functions.

### Sub-components
- **aes**: Supports AES-128/192/256, GCM mode (recommended) and CBC mode
- **rsa**: Supports encryption, decryption, signing, verification, PEM format keys
- **bcrypt**: Password hashing and verification

### Features
- Environment variable configuration for keys
- GCM mode provides authenticated encryption
- RSA supports multiple padding modes

### Usage
For usage examples, refer to [gcrypto usage](gcrypto/README.md)

## gerror

### Overview
`gerror` is an error handling component providing business error code encapsulation, supporting error chains and call stacks.

### Features
- Supports errors.Is/As
- Error chain wrapping
- Call stack recording
- Business error code specification

## glog

### Overview
`glog` is a logging component based on zap providing high-performance logging functionality.

### Features
- Console/File output support
- OTel integration
- Structured logging support
- High-performance log writing

## gtrace

### Overview
`gtrace` is an OpenTelemetry Trace initialization component supporting distributed tracing.

### Features
- OTLP gRPC/HTTP export support
- Exporter disable mechanism
- Integrated zap logging

### Usage
For usage examples, refer to [gtrace usage](gtrace/README.md)

## gtree

### Overview
`gtree` is a tree structure construction tool, a generic tree data structure building library supporting building trees from node lists.

### Features
- Provides TreeNode interface, only need to implement GetKey(), GetParentKey(), IsRoot() methods
- Orphan node handling (ignore, promote to root, error)
- Circular reference detection
- Node sorting (ID, Name, Order or multi-level combination)
- Pre-order traversal and level-order traversal

## gutil

### Overview
`gutil` is a collection of common utility functions providing commonly used tool functions during development.

### Sub-components
- Random number generation
- String processing
- Date/time operations
- Type conversion
- Slice/Map operations
- File processing

## protocol

### Overview
`protocol` is a protocol-related component collection providing HTTP client encapsulation.

### Sub-components
- **ghttp**: Enhanced HTTP client, supports struct mapping, connection pool, smart retry and other features
- **gresty**: HTTP client wrapper based on Resty, supports SSE (Server-Sent Events)

### Features
- Struct automatic mapping support
- Connection pool optimization
- Smart retry mechanism (no retry for 4xx, retry for 5xx)
- SSE long connection support
- Rich configuration options

### Usage
For usage examples, refer to [ghttp usage](protocol/ghttp/README.md)

## storage

### Overview
`storage` is a unified object storage component supporting multiple cloud providers with a consistent API.

### Supported Providers
- AWS S3
- MinIO
- Alibaba Cloud OSS
- Tencent Cloud COS
- Volcano Engine TOS

### Features
- Unified API across all providers
- Multipart upload support
- Presigned URL generation (GET/PUT)
- Object listing with paginator
- Batch operations (delete, copy)
- URI helper for standardized resource identifiers
- Key builder with prefix, date layout, and random suffix

### Usage
For usage examples, refer to [storage usage](storage/README.md)

## ratelimit

### Overview
`ratelimit` is a rate limiting component supporting Redis-based and local time window/token bucket rate limiting.

### Features
- Redis rate limiting (go-redis-rate)
- Local rate limiting (timeRateLimiter)
- Degradation handling support