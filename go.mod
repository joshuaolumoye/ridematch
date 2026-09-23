module ridematch-backend

go 1.24.7

require (
	github.com/aws/aws-sdk-go-v2 v1.32.6
	github.com/aws/aws-sdk-go-v2/credentials v1.17.47
	github.com/aws/aws-sdk-go-v2/service/s3 v1.66.3
	github.com/gin-gonic/gin v1.7.7
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.5.1
	golang.org/x/crypto v0.22.0
	gorm.io/driver/mysql v1.5.6
	gorm.io/gorm v1.25.9
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.25 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.25 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.4.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.4 // indirect
	github.com/aws/smithy-go v1.22.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.19.0 // indirect
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/golang/protobuf v1.3.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.9 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180228061459-e0a39a4cb421 // indirect
	github.com/modern-go/reflect2 v0.0.0-20180701023420-4b7aa43c6742 // indirect
	github.com/ugorji/go/codec v1.1.7 // indirect
	golang.org/x/net v0.21.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)

replace (
	golang.org/x/arch => github.com/golang/arch v0.7.0
	golang.org/x/crypto => github.com/golang/crypto v0.22.0
	golang.org/x/mod => github.com/golang/mod v0.8.0
	golang.org/x/net => github.com/golang/net v0.24.0
	golang.org/x/sync => github.com/golang/sync v0.1.0
	golang.org/x/sys => github.com/golang/sys v0.19.0
	golang.org/x/term => github.com/golang/term v0.19.0
	golang.org/x/text => github.com/golang/text v0.14.0
	golang.org/x/tools => github.com/golang/tools v0.6.0
	google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.34.0
	gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20161208181325-20d25e280405
	gopkg.in/yaml.v2 => github.com/go-yaml/yaml/v2 v2.2.8
	gopkg.in/yaml.v3 => github.com/go-yaml/yaml/v3 v3.0.1
	gorm.io/driver/mysql => github.com/go-gorm/mysql v1.5.6
	gorm.io/gorm => github.com/go-gorm/gorm v1.25.9
)
