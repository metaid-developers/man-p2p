mac:
	GOOS=linux GOARCH=amd64 \
	CC=x86_64-unknown-linux-gnu-gcc \
	CXX=x86_64-unknown-linux-gnu-g++ \
	CGO_LDFLAGS="-L/usr/local/x86_64-linux/lib -lzmq" \
	CGO_ENABLED=1 \
	go build

linux:
	GOOS=linux GOARCH=amd64 \
	CGO_ENABLED=1 \
	go build
mrc20_migration:
	GOOS=linux GOARCH=amd64 \
	CGO_ENABLED=0 \
	go build -o mrc20_migration ./tools/cmd/mrc20_migration.go
run_regtest:
	CGO_ENABLED=1 go run app.go -test=2 -config=./config_regtest.toml