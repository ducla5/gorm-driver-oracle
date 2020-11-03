# GORM Oracle Driver

This driver is under development.
Welcome PR

## Quick Start

```go
import (
  "github.com/ducla5/gorm-driver-oracle"
  "gorm.io/gorm"
)

dsn := "gorm/gorm@localhost"
db, err = gorm.Open(oracle.Open(connectString), &gorm.Config{})
```

## Configuration

```go
import (
  "github.com/ducla5/gorm-driver-oracle"
  "gorm.io/gorm"
)

db, err := gorm.Open(oracle.New(oracle.Config{
  DSN: "gorm/gorm@localhost",
  DriverName        "godror",
  DefaultStringSize 251,

}), &gorm.Config{})
```


Checkout [https://gorm.io](https://gorm.io) for details.
