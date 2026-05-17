# OpenTelemetry Compile-Time Instrumentation Compatibility Report

Generated automatically by the compatibility testing toolchain.

## Target Packages Compatibility Matrix

The following matrix documents the verified dependency versions supported by the `otelc-next` compiler pipeline, along with matching rules and validation status.

| Target Package | Match Rule Name | Checked Versions | Injection Pattern | Validation Status |
| :--- | :--- | :--- | :--- | :--- |
| **github.com/gin-gonic/gin** | `gin-http-server` | `v1.7.0` - `v1.9.1` | `before_after` (deferred span wrap) | **[PASS] Fully Validated** |
| **github.com/jackc/pgx/v5** | `pgx-conn-query` | `v5.0.0` - `v5.5.2` | `before_after` (span statement query) | **[PASS] Fully Validated** |
| **gorm.io/gorm** | `gorm-open` | `v1.20.0` - `v1.25.7` | `plugin_register` (hook callback) | **[PASS] Fully Validated** |
| **github.com/go-redis/redis/v8** | `redis-client` | `v8.0.0` - `v8.11.5` | `plugin_register` (OTel hook install) | **[PASS] Fully Validated** |
| **github.com/segmentio/kafka-go**| `kafka-producer` | `v0.4.0` - `v0.4.47` | `before_after` (publish context span) | **[PASS] Fully Validated** |
| **github.com/sirupsen/logrus** | `logrus-setup` | `v1.8.0` - `v1.9.3` | `plugin_register` (Log hook install) | **[PASS] Fully Validated** |
| **log/slog** | `slog-handler` | Go 1.21 - Go 1.26 | `plugin_register` (Handler wrapping) | **[PASS] Fully Validated** |
| **k8s.io/client-go/tools/cache**| `k8sclient-informer` | `v0.20.0` - `v0.29.2` | `plugin_register` (Workqueue span) | **[PASS] Fully Validated** |
| **github.com/sashabaranov/go-openai**| `openai-completion` | `v1.5.0` - `v1.20.4` | `before_after` (Usage span) | **[PASS] Fully Validated** |
| **github.com/tmc/langchaingo/chains**| `langchaingo-chain`| `v0.1.0` - `v0.1.5` | `before_after` (Chain call span) | **[PASS] Fully Validated** |

## Compiler Infrastructure Verification

- **Go Compiler Toolchain Compatibility**: Go 1.22, 1.23, 1.24, 1.25, 1.26 (windows/amd64, linux/amd64, darwin/arm64)
- **Decorated Syntax Tree (DST) Engine Integrity**: Confirmed zero-loss preservation of code formatting, comments, and spacing layouts.
- **-toolexec Argument Interception Resolution**: Tested across incremental, clean, and parallel build pipelines.

---
*Report generated on Sunday, 17-May-2026. All telemetry systems conform to OTel Semantic Conventions v1.24.0.*
