\# mtlog - Message Template Logging for Go



\[!\[Go Reference](https://pkg.go.dev/badge/github.com/willibrandon/mtlog.svg)](https://pkg.go.dev/github.com/willibrandon/mtlog)

\[!\[License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)



mtlog is a high-performance structured logging library for Go, inspired by \[Serilog](https://serilog.net/). It brings message templates and pipeline architecture to the Go ecosystem, achieving zero allocations for simple logging operations while providing powerful features for complex scenarios.



\## Features



\### Core Features

\- \*\*Zero-allocation logging\*\* for simple messages (17.3 ns/op)

\- \*\*Message templates\*\* with positional property extraction and format specifiers

\- \*\*Go template syntax\*\* support (`{{.Property}}`) alongside traditional syntax

\- \*\*OpenTelemetry compatibility\*\* with support for dotted property names (`{http.method}`, `{service.name}`)

\- \*\*Structured fields\*\* via `With()` method for slog-style key-value pairs

\- \*\*Output templates\*\* for customizable log formatting

\- \*\*Per-message sampling\*\* with multiple strategies (counter, rate, time, adaptive) for intelligent log volume control

\- \*\*ForType logging\*\* with automatic SourceContext from Go types and intelligent caching

\- \*\*LogContext scoped properties\*\* that flow through operation contexts

\- \*\*Source context enrichment\*\* with intelligent caching for automatic logger categorization

\- \*\*Context deadline awareness\*\* with automatic timeout warnings and deadline tracking

\- \*\*Pipeline architecture\*\* for clean separation of concerns

\- \*\*Type-safe generics\*\* for better compile-time safety

\- \*\*LogValue interface\*\* for safe logging of sensitive data

\- \*\*SelfLog diagnostics\*\* for debugging silent failures and configuration issues

\- \*\*Standard library compatibility\*\* via slog.Handler adapter (Go 1.21+)

\- \*\*Kubernetes ecosystem\*\* support via logr.LogSink adapter



\### HTTP Middleware

\- \*\*Multi-framework support\*\* for net/http, Gin, Echo, Fiber, and Chi

\- \*\*High-performance request/response logging\*\* with minimal overhead (~2.3μs per request)

\- \*\*Object pooling\*\* for zero-allocation paths in high-throughput scenarios

\- \*\*Advanced sampling strategies\*\* (rate, adaptive, path-based) for log volume control

\- \*\*Request body logging\*\* with automatic sanitization of sensitive data

\- \*\*Distributed tracing\*\* with W3C Trace Context, B3, and X-Ray format support

\- \*\*Health check handlers\*\* with configurable metrics and liveness/readiness probes

\- \*\*Panic recovery\*\* with detailed stack traces and custom error handling



\### Sinks \& Output

\- \*\*Console sink\*\* with customizable themes (dark, light, ANSI, Literate)

\- \*\*File sink\*\* with rolling policies (size, time-based)

\- \*\*Seq integration\*\* with CLEF format and dynamic level control

\- \*\*Elasticsearch sink\*\* for centralized log storage and search

\- \*\*Splunk sink\*\* with HEC (HTTP Event Collector) support

\- \*\*OpenTelemetry (OTLP) sink\*\* with gRPC/HTTP transport, batching, and trace correlation

\- \*\*Sentry integration\*\* with error tracking, performance monitoring, and intelligent sampling

\- \*\*Conditional sink\*\* for predicate-based routing with zero overhead

\- \*\*Router sink\*\* for multi-destination routing with FirstMatch/AllMatch modes

\- \*\*Async sink wrapper\*\* for high-throughput scenarios

\- \*\*Durable buffering\*\* with persistent storage for reliability



\### Pipeline Components

\- \*\*Rich enrichment\*\* with built-in and custom enrichers

\- \*\*Advanced filtering\*\* including rate limiting and sampling

\- \*\*Minimum level overrides\*\* by source context patterns

\- \*\*Type-safe capturing\*\* with caching for performance

\- \*\*Dynamic level control\*\* with runtime adjustments

\- \*\*Configuration from JSON\*\* for flexible deployment



\## Installation



```bash

go get github.com/willibrandon/mtlog

```



\## Quick Start



```go

package main



import (

&nbsp;   "github.com/willibrandon/mtlog"

&nbsp;   "github.com/willibrandon/mtlog/core"

)



func main() {

&nbsp;   // Create a logger with console output

&nbsp;   log := mtlog.New(

&nbsp;       mtlog.WithConsole(),

&nbsp;       mtlog.WithMinimumLevel(core.InformationLevel),

&nbsp;   )



&nbsp;   // Simple logging

&nbsp;   log.Info("Application started")

&nbsp;   

&nbsp;   // Message templates with properties

&nbsp;   userId := 123

&nbsp;   log.Info("User {UserId} logged in", userId)

&nbsp;   

&nbsp;   // Capturing complex types

&nbsp;   order := Order{ID: 456, Total: 99.95}

&nbsp;   log.Info("Processing {@Order}", order)

}



// For libraries that need error handling:

func NewLibraryLogger() (\*mtlog.Logger, error) {

&nbsp;   return mtlog.Build(

&nbsp;       mtlog.WithConsoleTemplate("\[${Timestamp:HH:mm:ss} ${Level:u3}] ${Message}"),

&nbsp;       mtlog.WithMinimumLevel(core.DebugLevel),

&nbsp;   )

}

```



\## Visual Example



!\[mtlog with Literate theme](assets/literate-theme.png)



\## Message Templates



mtlog uses message templates that preserve structure throughout the logging pipeline:



```go

// Properties are extracted positionally

log.Information("User {UserId} logged in from {IP}", userId, ipAddress)



// Go template syntax is also supported

log.Information("User {{.UserId}} logged in from {{.IP}}", userId, ipAddress)



// OTEL-style dotted properties for compatibility with OpenTelemetry conventions

log.Information("HTTP {http.method} to {http.url} returned {http.status\_code}", "GET", "/api", 200)

log.Information("Service {service.name} in {service.namespace}", "api", "production")



// Mix both syntaxes as needed

log.Information("User {UserId} ({{.Username}}) from {IP}", userId, username, ipAddress)



// Capturing hints:

// @ - capture complex types into properties

log.Information("Order {@Order} created", order)



// $ - force scalar rendering (stringify)

log.Information("Error occurred: {$Error}", err)



// Format specifiers

log.Information("Processing time: {Duration:F2}ms", 123.456)

log.Information("Disk usage at {Percentage:P1}", 0.85)  // 85.0%

log.Information("Order {OrderId:000} total: ${Amount:F2}", 42, 99.95)



// String formatting - default is no quotes (Go-idiomatic)

log.Information("User {Name} logged in", "Alice")  // User Alice logged in

log.Information("User {Name:q} logged in", "Alice")  // User "Alice" logged in (explicit quotes)

log.Information("User {Name:l} logged in", "Alice")  // User Alice logged in (same as default)



// JSON formatting - outputs any value as JSON

log.Information("Config: {Settings:j}", map\[string]any{"debug": true, "port": 8080})

// Config: {"debug":true,"port":8080}



// Numeric indexing (like string.Format in .NET)

log.Information("Processing {0} of {1} items", 5, 10)

log.Information("The {0} {1} {2} jumped over the {3} {4}", 

&nbsp;   "quick", "brown", "fox", "lazy", "dog")

```



\### Numeric Indexing



mtlog supports numeric indexing similar to .NET's `string.Format` and Serilog:



```go

// Pure numeric indexing uses index values (like string.Format)

log.Information("Processing {0} of {1}", 5, 10)  // Processing 5 of 10

log.Information("Result: {1} before {0}", "first", "second")  // Result: second before first



// Mixed named and numeric uses positional matching (left-to-right)

log.Information("User {UserId} processed {0} of {1}", 123, 50, 100)

// UserId=123 (1st arg), 0=50 (2nd arg), 1=100 (3rd arg)



// Note: Avoid mixing named and numeric properties for clarity

```



\## Output Templates



Control how log events are formatted for output with customizable templates. Output templates use `${...}` syntax for built-in elements to distinguish them from message template properties:



```go

// Console with custom output template and theme

log := mtlog.New(

&nbsp;   mtlog.WithConsoleTemplateAndTheme(

&nbsp;       "\[${Timestamp:HH:mm:ss} ${Level:u3}] {SourceContext}: ${Message}",

&nbsp;       sinks.LiterateTheme(),

&nbsp;   ),

)



// File with detailed template

log := mtlog.New(

&nbsp;   mtlog.WithFileTemplate("app.log", 

&nbsp;       "\[${Timestamp:yyyy-MM-dd HH:mm:ss.fff zzz} ${Level:u3}] {SourceContext}: ${Message}${NewLine}${Exception}"),

)

```



\### Template Properties

\- `${Timestamp}` - Event timestamp with optional format

\- `${Level}` - Log level with format options (u3, u, l)

\- `${Message}` - Rendered message from template

\- `{SourceContext}` - Logger context/category

\- `${Exception}` - Exception details if present

\- `${NewLine}` - Platform-specific line separator

\- Custom properties by name: `{RequestId}`, `{UserId}`, etc.



\### Format Specifiers

\- \*\*Timestamps\*\*: `HH:mm:ss`, `yyyy-MM-dd`, `HH:mm:ss.fff`

\- \*\*Levels\*\*: 

&nbsp; - `u3` or `w3` - Three-letter uppercase (INF, WRN, ERR)

&nbsp; - `u` - Full uppercase (INFORMATION, WARNING, ERROR)

&nbsp; - `w` - Full lowercase (information, warning, error)

&nbsp; - `l` - Lowercase three-letter (inf, wrn, err) \[deprecated, use w3]

\- \*\*Numbers\*\*: `000` (zero-pad), `F2` (2 decimals), `P1` (percentage)

\- \*\*Strings\*\*: `:l` removes quotes (literal format)



\### Design Note: Why `${...}` for Built-ins?



Unlike Serilog which uses `{...}` for both built-in elements and properties in output templates, mtlog uses `${...}` for built-ins. This design choice prevents ambiguity when user properties have the same names as built-in elements (e.g., a property named "Message" would conflict with the built-in {Message}).



The `${...}` syntax provides clear disambiguation:

\- `${Message}`, `${Timestamp}`, `${Level}` - Built-in template elements

\- `{UserId}`, `{OrderId}`, `{Message}` - User properties from your log events



This means you can safely log a property called "Message" without conflicts:

```go

log.Information("Processing {Message} from {Queue}", userMessage, queueName)

// Output template: "\[${Timestamp}] ${Level}: ${Message}"

// Result: "\[2024-01-15] INF: Processing Hello World from orders"

```



\## Pipeline Architecture



The logging pipeline processes events through distinct stages:



```

Message Template Parser → Enrichment → Filtering → Capturing → Output

```



\### Configuration with Functional Options



```go

log := mtlog.New(

&nbsp;   // Output configuration

&nbsp;   mtlog.WithConsoleTheme("dark"),     // Console with dark theme

&nbsp;   mtlog.WithRollingFile("app.log", 10\*1024\*1024), // Rolling file (10MB)

&nbsp;   mtlog.WithSeq("http://localhost:5341", "api-key"), // Seq integration

&nbsp;   

&nbsp;   // Enrichment

&nbsp;   mtlog.WithTimestamp(),              // Add timestamp

&nbsp;   mtlog.WithMachineName(),            // Add hostname

&nbsp;   mtlog.WithProcessInfo(),            // Add process ID/name

&nbsp;   mtlog.WithCallersInfo(),            // Add file/line info

&nbsp;   

&nbsp;   // Filtering \& Level Control

&nbsp;   mtlog.WithMinimumLevel(core.DebugLevel),

&nbsp;   mtlog.WithDynamicLevel(levelSwitch), // Runtime level control

&nbsp;   mtlog.WithFilter(customFilter),

&nbsp;   

&nbsp;   // Capturing

&nbsp;   mtlog.WithCapturing(),          // Enable @ hints

&nbsp;   mtlog.WithCapturingDepth(5),    // Max depth

)

```



\## Enrichers



Enrichers add contextual information to all log events:



```go

// Built-in enrichers

log := mtlog.New(

&nbsp;   mtlog.WithTimestamp(),

&nbsp;   mtlog.WithMachineName(),

&nbsp;   mtlog.WithProcessInfo(),

&nbsp;   mtlog.WithEnvironmentVariables("APP\_ENV", "VERSION"),

&nbsp;   mtlog.WithThreadId(),

&nbsp;   mtlog.WithCallersInfo(),

&nbsp;   mtlog.WithCorrelationId("RequestId"),

&nbsp;   mtlog.WithSourceContext(), // Auto-detect logger context

)



// Structured fields with With() - slog-style

log.With("service", "auth", "version", "1.0").Info("Service started")

log.With("user\_id", 123, "request\_id", "abc-123").Info("Processing request")



// Context-based enrichment

ctx := context.WithValue(context.Background(), "RequestId", "abc-123")

log.ForContext("UserId", userId).Information("Processing request")



// Source context for sub-loggers

serviceLog := log.ForSourceContext("MyApp.Services.UserService")

serviceLog.Information("User service started")



// Type-based loggers with automatic SourceContext

userLogger := mtlog.ForType\[User](log)

userLogger.Information("User operation") // SourceContext: "User"



orderLogger := mtlog.ForType\[OrderService](log)

orderLogger.Information("Processing order") // SourceContext: "OrderService"

```



\## Per-Message Sampling



Efficient log volume management through intelligent per-message sampling. mtlog provides comprehensive sampling capabilities to help control log volume in production while preserving important events.



\### Quick Examples



```go

// Basic sampling methods

logger.Sample(10).Info("Every 10th message")

logger.SampleRate(0.2).Info("20% of messages")

logger.SampleDuration(time.Second).Info("Once per second")

logger.SampleFirst(100).Info("First 100 only")



// Adaptive sampling - maintains target events/second

logger.SampleAdaptive(100).Info("Auto-adjusting rate")



// Use predefined profiles

logger.SampleProfile("HighTrafficAPI").Info("API call")

logger.SampleProfile("ProductionErrors").Error("Error occurred")

```



\### Key Features



\- \*\*Multiple Strategies\*\*: Counter, rate, time-based, first-N, group, conditional, and exponential backoff sampling

\- \*\*Adaptive Sampling\*\*: Automatically adjusts rates to maintain target throughput with hysteresis and dampening

\- \*\*Predefined Profiles\*\*: Ready-to-use configurations for common scenarios

\- \*\*Custom Profiles\*\*: Define and register your own reusable sampling configurations

\- \*\*Version Management\*\*: Support for versioned profiles with auto-migration

\- \*\*Zero Allocations\*\*: Optimized for minimal performance impact



\### Learn More



For comprehensive documentation including advanced strategies, configuration options, and best practices, see the \[\*\*Sampling Guide\*\*](docs/sampling-guide.md).



\## Structured Fields with With()



The `With()` method provides a convenient way to add structured fields to log events, following the slog convention of accepting variadic key-value pairs:



```go

// Basic usage with key-value pairs

logger.With("service", "api", "version", "1.0").Info("Service started")



// Chaining With() calls

logger.

&nbsp;   With("environment", "production").

&nbsp;   With("region", "us-west-2").

&nbsp;   Info("Deployment complete")



// Create a base logger with common fields

apiLogger := logger.With(

&nbsp;   "component", "api",

&nbsp;   "host", "api-server-01",

)



// Reuse the base logger for multiple operations

apiLogger.Info("Handling request")

apiLogger.With("endpoint", "/users").Info("GET /users")

apiLogger.With("endpoint", "/products", "method", "POST").Info("POST /products")



// Request-scoped logging

requestLogger := apiLogger.With(

&nbsp;   "request\_id", "abc-123",

&nbsp;   "user\_id", 456,

)

requestLogger.Info("Request started")

requestLogger.With("duration\_ms", 42).Info("Request completed")



// Combine With() and ForContext()

logger.

&nbsp;   With("service", "payment").

&nbsp;   ForContext("transaction\_id", "tx-789").

&nbsp;   With("amount", 99.99, "currency", "USD").

&nbsp;   Info("Payment processed")

```



\### With() vs ForContext()



\- \*\*With()\*\*: Accepts variadic key-value pairs (slog-style), convenient for multiple fields

\- \*\*ForContext()\*\*: Takes a single property name and value, returns a new logger

\- Both methods create a new logger instance with the combined properties

\- Both are safe for concurrent use



\### Property Precedence



When combining With() and ForContext(), properties follow a precedence order:

\- Properties passed directly to log methods take highest precedence

\- ForContext() properties override With() properties

\- Later With() calls override earlier With() calls in a chain



Example:

```go

logger.With("user", "alice").              // user=alice

&nbsp;   ForContext("user", "bob").             // user=bob (ForContext overrides)

&nbsp;   With("user", "charlie").               // user=charlie (later With overrides)

&nbsp;   Info("User {user} logged in", "david") // user=david (event property overrides all)

```



\## LogContext - Scoped Properties



LogContext provides a way to attach properties to a context that will be automatically included in all log events created from loggers using that context. Properties follow a precedence order: event-specific properties (passed directly to log methods) override ForContext properties, which override LogContext properties (set via PushProperty).



```go

// Add properties to context that flow through all operations

ctx := context.Background()

ctx = mtlog.PushProperty(ctx, "RequestId", "req-12345")

ctx = mtlog.PushProperty(ctx, "UserId", userId)

ctx = mtlog.PushProperty(ctx, "TenantId", "acme-corp")



// Create a logger that includes context properties

log := logger.WithContext(ctx)

log.Information("Processing request") // Includes all pushed properties



// Properties are inherited - child contexts get parent properties

func processOrder(ctx context.Context, orderId string) {

&nbsp;   // Add operation-specific properties

&nbsp;   ctx = mtlog.PushProperty(ctx, "OrderId", orderId)

&nbsp;   ctx = mtlog.PushProperty(ctx, "Operation", "ProcessOrder")

&nbsp;   

&nbsp;   log := logger.WithContext(ctx)

&nbsp;   log.Information("Order processing started") // Includes all parent + new properties

}



// Property precedence example

ctx = mtlog.PushProperty(ctx, "UserId", 123)

logger.WithContext(ctx).Information("Test")                          // UserId=123

logger.WithContext(ctx).ForContext("UserId", 456).Information("Test") // UserId=456 (ForContext overrides)

logger.WithContext(ctx).Information("User {UserId}", 789)            // UserId=789 (event property overrides all)

```



This is particularly useful for:

\- Request tracing in web applications

\- Maintaining context through async operations

\- Multi-tenant applications

\- Batch processing with job-specific context



\## ForType - Type-Based Logging



ForType provides automatic SourceContext from Go types, making it easy to categorize logs by the types they relate to without manual string constants:



```go

// Automatic SourceContext from type names

userLogger := mtlog.ForType\[User](logger)

userLogger.Information("User created") // SourceContext: "User"



productLogger := mtlog.ForType\[Product](logger)

productLogger.Information("Product updated") // SourceContext: "Product"



// Works with pointers (automatically dereferenced)

mtlog.ForType\[\*User](logger).Information("User updated") // SourceContext: "User"



// Service-based logging

type UserService struct {

&nbsp;   logger core.Logger

}



func NewUserService(baseLogger core.Logger) \*UserService {

&nbsp;   return \&UserService{

&nbsp;       logger: mtlog.ForType\[UserService](baseLogger),

&nbsp;   }

}



func (s \*UserService) CreateUser(name string) {

&nbsp;   s.logger.Information("Creating user {Name}", name)

&nbsp;   // All logs from this service have SourceContext: "UserService"

}

```



\### Advanced Type Naming



For more control over type names, use `ExtractTypeName` with `TypeNameOptions`:



```go

// Include package for disambiguation

opts := mtlog.TypeNameOptions{

&nbsp;   IncludePackage: true,

&nbsp;   PackageDepth:   1, // Only immediate package

}

name := mtlog.ExtractTypeName\[User](opts) // Result: "myapp.User"

logger := baseLogger.ForContext("SourceContext", name)



// Add prefixes for microservice identification

opts = mtlog.TypeNameOptions{Prefix: "UserAPI."}

name = mtlog.ExtractTypeName\[User](opts) // Result: "UserAPI.User"



// Simplify anonymous structs

opts = mtlog.TypeNameOptions{SimplifyAnonymous: true}

name = mtlog.ExtractTypeName\[struct{ Name string }](opts) // Result: "AnonymousStruct"



// Disable warnings for production

opts = mtlog.TypeNameOptions{WarnOnUnknown: false}

name = mtlog.ExtractTypeName\[interface{}](opts) // Result: "Unknown" (no warning logged)



// Combine multiple options

opts = mtlog.TypeNameOptions{

&nbsp;   IncludePackage:    true,

&nbsp;   PackageDepth:      1,

&nbsp;   Prefix:            "MyApp.",

&nbsp;   Suffix:            ".Handler",

&nbsp;   SimplifyAnonymous: true,

}

```



\### Performance \& Caching



ForType uses reflection with intelligent caching for optimal performance:



\- \*\*~7% overhead\*\* vs manual `ForSourceContext` (uncached)

\- \*\*~1% overhead\*\* with caching enabled (subsequent calls)

\- \*\*Thread-safe\*\* caching with `sync.Map`

\- \*\*Zero allocations\*\* for cached type names



```go

// Performance comparison

ForType\[User](logger).Information("User operation")           // ~7% slower than manual

logger.ForSourceContext("User").Information("User operation") // Baseline performance



// But subsequent ForType calls are nearly free due to caching



// Cache statistics for monitoring

stats := mtlog.GetTypeNameCacheStats()

fmt.Printf("Cache hits: %d, misses: %d, evictions: %d, hit ratio: %.1f%%, size: %d/%d", 

&nbsp;   stats.Hits, stats.Misses, stats.Evictions, stats.HitRatio, stats.Size, stats.MaxSize)



// For testing scenarios requiring cache isolation

mtlog.ResetTypeNameCache() // Clears cache and statistics

```



\### Multi-Tenant Support



For applications serving multiple tenants, ForType supports tenant-specific cache namespaces:



```go

// Multi-tenant type-based logging with separate cache namespaces

func CreateTenantLogger(baseLogger core.Logger, tenantID string) core.Logger {

&nbsp;   tenantPrefix := fmt.Sprintf("tenant:%s", tenantID)

&nbsp;   return mtlog.ForTypeWithCacheKey\[UserService](baseLogger, tenantPrefix)

}



// Each tenant gets separate cache entries

acmeLogger := CreateTenantLogger(logger, "acme-corp")    // Cache key: tenant:acme-corp + UserService

globexLogger := CreateTenantLogger(logger, "globex-inc") // Cache key: tenant:globex-inc + UserService



acmeLogger.Information("Processing acme user")   // SourceContext: "UserService" (acme cache)

globexLogger.Information("Processing globex user") // SourceContext: "UserService" (globex cache)



// Custom type naming per tenant

opts := mtlog.TypeNameOptions{Prefix: "AcmeCorp."}

acmeName := mtlog.ExtractTypeNameWithCacheKey\[User](opts, "tenant:acme")

// Result: "AcmeCorp.User" (cached separately per tenant)

```



\### Type Name Cache Configuration



The type name cache can be configured via environment variables:



```bash

\# Set cache size limit (default: 10,000 entries)

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=50000  # For large applications

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=1000   # For memory-constrained environments

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=0      # Disable caching entirely

```



The cache uses LRU (Least Recently Used) eviction when the size limit is exceeded, ensuring memory usage stays bounded while keeping frequently used type names cached.



This is particularly useful for:

\- Large applications with many service types

\- Type-safe logger categorization

\- Automatic SourceContext without string constants

\- Service-oriented architectures

\- Multi-tenant applications requiring cache isolation



\## Context Deadline Awareness



mtlog can automatically detect and warn when operations are approaching their context deadlines, helping catch timeout-related issues before they fail:



```go

// Configure deadline awareness

logger := mtlog.New(

&nbsp;   mtlog.WithConsole(),

&nbsp;   mtlog.WithContextDeadlineWarning(100\*time.Millisecond), // Warn within 100ms

)



// Use context-aware logging methods

ctx, cancel := context.WithTimeout(context.Background(), 500\*time.Millisecond)

defer cancel()



logger.InfoContext(ctx, "Starting operation")

time.Sleep(350 \* time.Millisecond)

logger.InfoContext(ctx, "Still processing...") // Warning: approaching deadline!



// Percentage-based thresholds

logger := mtlog.New(

&nbsp;   mtlog.WithDeadlinePercentageThreshold(

&nbsp;       1\*time.Millisecond,  // Minimum absolute threshold

&nbsp;       0.2,                 // Warn when 20% of time remains

&nbsp;   ),

)



// HTTP handler example

func handler(w http.ResponseWriter, r \*http.Request) {

&nbsp;   ctx, cancel := context.WithTimeout(r.Context(), 200\*time.Millisecond)

&nbsp;   defer cancel()

&nbsp;   

&nbsp;   logger.InfoContext(ctx, "Processing request")

&nbsp;   // ... perform operations ...

&nbsp;   logger.InfoContext(ctx, "Response ready") // Warns if close to timeout

}

```



Features:

\- \*\*Zero overhead\*\* when no deadline is present (2.7ns, 0 allocations)

\- \*\*Automatic level upgrading\*\* - Info logs become Warnings when deadline approaches

\- \*\*OTEL-style properties\*\* - `deadline.remaining\_ms`, `deadline.at`, `deadline.approaching`

\- \*\*First warning tracking\*\* - Marks the first warning for each context

\- \*\*Deadline exceeded detection\*\* - Tracks operations that continue past deadline

\- \*\*LRU cache with TTL\*\* - Efficient tracking with automatic cleanup

\- \*\*Custom handlers\*\* - Add metrics, alerts, or custom logic when deadlines approach



\## Filters



Control which events are logged with powerful filtering:



```go

// Level filtering

mtlog.WithMinimumLevel(core.WarningLevel)



// Minimum level overrides by source context

mtlog.WithMinimumLevelOverrides(map\[string]core.LogEventLevel{

&nbsp;   "github.com/gin-gonic/gin":       core.WarningLevel,    // Suppress Gin info logs

&nbsp;   "github.com/go-redis/redis":      core.ErrorLevel,      // Only Redis errors

&nbsp;   "myapp/internal/services":        core.DebugLevel,      // Debug for internal services

&nbsp;   "myapp/internal/services/auth":   core.VerboseLevel,    // Verbose for auth debugging

})



// Custom predicate

mtlog.WithFilter(filters.NewPredicateFilter(func(e \*core.LogEvent) bool {

&nbsp;   return !strings.Contains(e.MessageTemplate.Text, "health-check")

}))



// Rate limiting

mtlog.WithFilter(filters.NewRateLimitFilter(100, time.Minute))



// Statistical sampling

mtlog.WithFilter(filters.NewSamplingFilter(0.1)) // 10% of events



// Property-based filtering

mtlog.WithFilter(filters.NewExpressionFilter("UserId", 123))

```



\## Sinks



mtlog supports multiple output destinations with advanced features:



\### Console Sink with Themes



```go

// Literate theme - beautiful, easy on the eyes

mtlog.WithConsoleTheme(sinks.LiterateTheme())



// Dark theme (default)

mtlog.WithConsoleTheme(sinks.DarkTheme())



// Light theme

mtlog.WithConsoleTheme(sinks.LightTheme()) 



// Plain text (no colors)

mtlog.WithConsoleTheme(sinks.NoColorTheme())

```



\### File Sinks



```go

// Simple file output

mtlog.WithFileSink("app.log")



// Rolling file by size

mtlog.WithRollingFile("app.log", 10\*1024\*1024) // 10MB



// Rolling file by time

mtlog.WithRollingFileTime("app.log", time.Hour) // Every hour

```



\### Seq Integration



```go

// Basic Seq integration

mtlog.WithSeq("http://localhost:5341")



// With API key

mtlog.WithSeq("http://localhost:5341", "your-api-key")



// Advanced configuration

mtlog.WithSeqAdvanced("http://localhost:5341",

&nbsp;   sinks.WithSeqBatchSize(100),

&nbsp;   sinks.WithSeqBatchTimeout(5\*time.Second),

&nbsp;   sinks.WithSeqCompression(true),

)



// Dynamic level control via Seq

levelOption, levelSwitch, controller := mtlog.WithSeqLevelControl(

&nbsp;   "http://localhost:5341",

&nbsp;   mtlog.SeqLevelControllerOptions{

&nbsp;       CheckInterval: 30\*time.Second,

&nbsp;       InitialCheck: true,

&nbsp;   },

)

```



\### Elasticsearch Integration



```go

// Basic Elasticsearch

mtlog.WithElasticsearch("http://localhost:9200", "logs")



// With authentication

mtlog.WithElasticsearchAdvanced(

&nbsp;   \[]string{"http://localhost:9200"},

&nbsp;   elasticsearch.WithIndex("myapp-logs"),

&nbsp;   elasticsearch.WithAPIKey("api-key"),

&nbsp;   elasticsearch.WithBatchSize(100),

)

```



\### Splunk Integration



```go

// Splunk HEC integration

mtlog.WithSplunk("http://localhost:8088", "your-hec-token")



// Advanced Splunk configuration

mtlog.WithSplunkAdvanced("http://localhost:8088",

&nbsp;   sinks.WithSplunkToken("hec-token"),

&nbsp;   sinks.WithSplunkIndex("main"),

&nbsp;   sinks.WithSplunkSource("myapp"),

)

```



\### Sentry Integration



```go

import (

&nbsp;   "github.com/willibrandon/mtlog"

&nbsp;   "github.com/willibrandon/mtlog/adapters/sentry"

)



// Basic Sentry error tracking

sink, \_ := sentry.WithSentry("https://key@sentry.io/project")

log := mtlog.New(mtlog.WithSink(sink))



// With sampling for high-volume applications

sink, \_ := sentry.WithSentry("https://key@sentry.io/project",

&nbsp;   sentry.WithFixedSampling(0.1), // 10% sampling

)

log := mtlog.New(mtlog.WithSink(sink))



// Advanced configuration with performance monitoring

sink, \_ := sentry.WithSentry("https://key@sentry.io/project",

&nbsp;   sentry.WithEnvironment("production"),

&nbsp;   sentry.WithRelease("v1.2.3"),

&nbsp;   sentry.WithTracesSampleRate(0.2),

&nbsp;   sentry.WithProfilesSampleRate(0.1),

&nbsp;   sentry.WithAdaptiveSampling(0.01, 0.5), // 1% to 50% adaptive

&nbsp;   sentry.WithRetryPolicy(3, time.Second),

&nbsp;   sentry.WithStackTraceCache(1000),

)

log := mtlog.New(mtlog.WithSink(sink))

```



\### Async and Durable Sinks



```go

// Wrap any sink for async processing

mtlog.WithAsync(mtlog.WithFileSink("app.log"))



// Durable buffering (survives crashes)

mtlog.WithDurable(

&nbsp;   mtlog.WithSeq("http://localhost:5341"),

&nbsp;   sinks.WithDurableDirectory("./logs/buffer"),

&nbsp;   sinks.WithDurableMaxSize(100\*1024\*1024), // 100MB buffer

)

```



\### Event Routing with Conditional and Router Sinks



Route log events to different destinations based on their properties:



\#### Conditional Sink



Filter events based on predicates with zero overhead for non-matching events:



```go

// Create a conditional sink for critical alerts

alertSink, \_ := sinks.NewFileSink("alerts.log")

criticalAlertSink := sinks.NewConditionalSink(

&nbsp;   func(event \*core.LogEvent) bool {

&nbsp;       return event.Level >= core.ErrorLevel \&\& 

&nbsp;              event.Properties\["Alert"] != nil

&nbsp;   },

&nbsp;   alertSink,

)



// Use built-in predicates

auditSink := sinks.NewConditionalSink(

&nbsp;   sinks.PropertyPredicate("Audit"),

&nbsp;   auditFileSink,

)



// Combine predicates

complexFilter := sinks.NewConditionalSink(

&nbsp;   sinks.AndPredicate(

&nbsp;       sinks.LevelPredicate(core.ErrorLevel),

&nbsp;       sinks.PropertyPredicate("Critical"),

&nbsp;       sinks.PropertyValuePredicate("Environment", "production"),

&nbsp;   ),

&nbsp;   targetSink,

)



logger := mtlog.New(

&nbsp;   mtlog.WithSink(sinks.NewConsoleSink()),

&nbsp;   mtlog.WithSink(criticalAlertSink),

&nbsp;   mtlog.WithSink(auditSink),

)



// Only critical errors with Alert property go to alerts.log

logger.With("Alert", true).Error("Database connection lost")

```



\#### Router Sink



Advanced routing with multiple destinations and routing modes:



```go

// FirstMatch mode - exclusive routing (stops at first match)

router := sinks.NewRouterSink(sinks.FirstMatch,

&nbsp;   sinks.Route{

&nbsp;       Name:      "errors",

&nbsp;       Predicate: sinks.LevelPredicate(core.ErrorLevel),

&nbsp;       Sink:      errorSink,

&nbsp;   },

&nbsp;   sinks.Route{

&nbsp;       Name:      "warnings",

&nbsp;       Predicate: sinks.LevelPredicate(core.WarningLevel),

&nbsp;       Sink:      warningSink,

&nbsp;   },

)



// AllMatch mode - broadcast to all matching routes

router := sinks.NewRouterSink(sinks.AllMatch,

&nbsp;   sinks.MetricRoute("metrics", metricsSink),

&nbsp;   sinks.AuditRoute("audit", auditSink),

&nbsp;   sinks.ErrorRoute("errors", errorSink),

)



// With default sink for non-matching events

router := sinks.NewRouterSinkWithDefault(

&nbsp;   sinks.FirstMatch,

&nbsp;   defaultSink,

&nbsp;   routes...,

)



// Dynamic route management at runtime

router.AddRoute(sinks.Route{

&nbsp;   Name:      "debug",

&nbsp;   Predicate: func(e \*core.LogEvent) bool { 

&nbsp;       return e.Level <= core.DebugLevel 

&nbsp;   },

&nbsp;   Sink:      debugSink,

})

router.RemoveRoute("debug")



// Fluent route builder API

route := sinks.NewRoute("special-events").

&nbsp;   When(func(e \*core.LogEvent) bool {

&nbsp;       category, \_ := e.Properties\["Category"].(string)

&nbsp;       return category == "Special"

&nbsp;   }).

&nbsp;   To(specialSink)



logger := mtlog.New(

&nbsp;   mtlog.WithSink(router),

&nbsp;   mtlog.WithSink(sinks.NewConsoleSink()),

)

```



\## Dynamic Level Control



Control logging levels at runtime without restarting your application:



\### Manual Level Control



```go

// Create a level switch

levelSwitch := mtlog.NewLoggingLevelSwitch(core.InformationLevel)



logger := mtlog.New(

&nbsp;   mtlog.WithLevelSwitch(levelSwitch),

&nbsp;   mtlog.WithConsole(),

)



// Change level at runtime

levelSwitch.SetLevel(core.DebugLevel)



// Fluent interface

levelSwitch.Debug().Information().Warning()



// Check if level is enabled

if levelSwitch.IsEnabled(core.VerboseLevel) {

&nbsp;   // Expensive logging operation

}

```



\### Centralized Level Control with Seq



```go

// Automatic level synchronization with Seq server

options := mtlog.SeqLevelControllerOptions{

&nbsp;   CheckInterval: 30 \* time.Second,

&nbsp;   InitialCheck:  true,

}



loggerOption, levelSwitch, controller := mtlog.WithSeqLevelControl(

&nbsp;   "http://localhost:5341", options)

defer controller.Close()



logger := mtlog.New(loggerOption)



// Level changes in Seq UI automatically update your application

```



\## Configuration from JSON



Configure loggers using JSON for flexible deployments:



```go

// Load from JSON file

config, err := configuration.LoadFromFile("logging.json")

if err != nil {

&nbsp;   log.Fatal(err)

}



logger := config.CreateLogger()

```



Example `logging.json`:

```json

{

&nbsp;   "minimumLevel": "Information",

&nbsp;   "sinks": \[

&nbsp;       {

&nbsp;           "type": "Console",

&nbsp;           "theme": "dark"

&nbsp;       },

&nbsp;       {

&nbsp;           "type": "RollingFile",

&nbsp;           "path": "logs/app.log",

&nbsp;           "maxSize": "10MB"

&nbsp;       },

&nbsp;       {

&nbsp;           "type": "Seq",

&nbsp;           "serverUrl": "http://localhost:5341",

&nbsp;           "apiKey": "${SEQ\_API\_KEY}"

&nbsp;       }

&nbsp;   ],

&nbsp;   "enrichers": \["Timestamp", "MachineName", "ProcessInfo"]

}

```



\## Safe Logging with LogValue



Protect sensitive data with the LogValue interface:



```go

type User struct {

&nbsp;   ID       int

&nbsp;   Username string

&nbsp;   Password string // Never logged

}



func (u User) LogValue() interface{} {

&nbsp;   return map\[string]interface{}{

&nbsp;       "id":       u.ID,

&nbsp;       "username": u.Username,

&nbsp;       // Password intentionally omitted

&nbsp;   }

}



// Password won't appear in logs

user := User{ID: 1, Username: "alice", Password: "secret"}

log.Information("User logged in: {@User}", user)

```



\## Performance



Benchmark results on AMD Ryzen 9 9950X:



| Operation | mtlog | zap | zerolog | Winner |

|-----------|-------|-----|---------|---------|

| Simple string | 16.82 ns | 146.6 ns | 36.46 ns | \*\*mtlog\*\* |

| Filtered out | 1.47 ns | 3.57 ns | 1.71 ns | \*\*mtlog\*\* |

| Two properties | 190.6 ns | 216.9 ns | 49.48 ns | zerolog |

| With context | 205.2 ns | 130.8 ns | 35.25 ns | zerolog |



\## Examples



See the \[examples](./examples) directory and adapter examples (\[OTEL](./adapters/otel/examples), \[Sentry](./adapters/sentry/examples), \[middleware](./adapters/middleware/examples)) for complete examples:



\- \[Basic logging](./examples/basic/main.go)

\- \[Using enrichers](./examples/enrichers/main.go)

\- \[Context logging](./examples/context/main.go)

\- \[Type-based logging](./examples/fortype/main.go)

\- \[LogContext scoped properties](./examples/logcontext/main.go)

\- \[Deadline awareness](./examples/deadline-awareness/main.go)

\- \[Advanced filtering](./examples/filtering/main.go)

\- \[Conditional logging](./examples/conditional/main.go)

\- \[Sampling basics](./examples/sampling/main.go)

\- \[Advanced sampling](./examples/sampling-advanced/main.go)

\- \[Sampling monitoring](./examples/sampling-monitoring/main.go)

\- \[Sampling debug](./examples/sampling-debug/main.go)

\- \[Sampling profiles](./examples/sampling-profiles/main.go)

\- \[Router sinks](./examples/router/main.go)

\- \[Capturing](./examples/capturing/main.go)

\- \[LogValue interface](./examples/logvalue/main.go)

\- \[Console themes](./examples/themes/main.go)

\- \[Output templates](./examples/output-templates/main.go)

\- \[Go template syntax](./examples/go-templates/main.go)

\- \[Rolling files](./examples/rolling/main.go)

\- \[Seq integration](./examples/seq/main.go)

\- \[Elasticsearch](./examples/elasticsearch/main.go)

\- \[Splunk integration](./examples/splunk/main.go)

\- \[OpenTelemetry basics](./adapters/otel/examples/simple/main.go)

\- \[OTEL with metrics](./adapters/otel/examples/metrics/main.go)

\- \[OTEL with sampling](./adapters/otel/examples/sampling/main.go)

\- \[OTEL with TLS](./adapters/otel/examples/tls/main.go)

\- \[Sentry error tracking](./adapters/sentry/examples/basic/main.go)

\- \[Sentry with context](./adapters/sentry/examples/context/main.go)

\- \[Sentry breadcrumbs](./adapters/sentry/examples/breadcrumbs/main.go)

\- \[Sentry with retry](./adapters/sentry/examples/retry/main.go)

\- \[Sentry performance monitoring](./adapters/sentry/examples/performance/main.go)

\- \[Sentry metrics dashboard](./adapters/sentry/examples/metrics/main.go)

\- \[Sentry sampling strategies](./adapters/sentry/examples/sampling/main.go)

\- \[Async logging](./examples/async/main.go)

\- \[Durable buffering](./examples/durable/main.go)

\- \[Dynamic levels](./examples/dynamic-levels/main.go)

\- \[Configuration](./examples/configuration/main.go)

\- \[Generics usage](./examples/generics/main.go)

\- \[With properties](./examples/with/main.go)

\- \[Showcase](./examples/showcase/main.go)

\- \[HTTP middleware](./adapters/middleware/examples/) - net/http, Gin, Echo, Fiber, Chi



\## Ecosystem Compatibility



\### HTTP Middleware



mtlog provides HTTP middleware adapters for popular Go web frameworks:



```go

import (

&nbsp;   "github.com/willibrandon/mtlog"

&nbsp;   "github.com/willibrandon/mtlog/adapters/middleware"

)



logger := mtlog.New(mtlog.WithConsole())



// net/http

mw := middleware.Middleware(middleware.DefaultOptions(logger))

handler := mw(yourHandler)



// Gin

router := gin.New()

router.Use(middleware.Gin(logger))



// Echo

e := echo.New()

e.Use(middleware.Echo(logger))



// Fiber

app := fiber.New()

app.Use(middleware.Fiber(logger))



// Chi

r := chi.NewRouter()

r.Use(middleware.Chi(logger))

```



Features include request/response logging, body capture with sanitization, distributed tracing, health checks, and object pooling for high-performance scenarios. See the \[HTTP Middleware Guide](./adapters/middleware/README.md) for detailed documentation.



\### Standard Library (slog)



mtlog provides full compatibility with Go's standard `log/slog` package:



```go

// Use mtlog as a backend for slog

slogger := mtlog.NewSlogLogger(

&nbsp;   mtlog.WithSeq("http://localhost:5341"),

&nbsp;   mtlog.WithMinimumLevel(core.InformationLevel),

)



// Use standard slog API

slogger.Info("user logged in", "user\_id", 123, "ip", "192.168.1.1")



// Or create a custom slog handler

logger := mtlog.New(mtlog.WithConsole())

slogger = slog.New(logger.AsSlogHandler())

```



\### Kubernetes (logr)



mtlog integrates with the Kubernetes ecosystem via logr:



```go

// Use mtlog as a backend for logr

import mtlogr "github.com/willibrandon/mtlog/adapters/logr"



logrLogger := mtlogr.NewLogger(

&nbsp;   mtlog.WithConsole(),

&nbsp;   mtlog.WithMinimumLevel(core.DebugLevel),

)



// Use standard logr API

logrLogger.Info("reconciling", "namespace", "default", "name", "my-app")

logrLogger.Error(err, "failed to update resource")



// Or create a custom logr sink

logger := mtlog.New(mtlog.WithSeq("http://localhost:5341"))

logrLogger = logr.New(logger.AsLogrSink())

```



\### OpenTelemetry (OTEL)



mtlog provides comprehensive OpenTelemetry integration with bidirectional support - use mtlog as an OTEL logger or send mtlog events to OTEL collectors:



```go

import "github.com/willibrandon/mtlog/adapters/otel"



// Basic OTLP sink with automatic trace correlation

logger := otel.NewOTELLogger(

&nbsp;   otel.WithOTLPEndpoint("localhost:4317"),

&nbsp;   otel.WithOTLPInsecure(), // For non-TLS connections

)



// Advanced configuration with batching and TLS

logger := mtlog.New(

&nbsp;   otel.WithOTLPSink(

&nbsp;       otel.WithOTLPEndpoint("otel-collector:4317"),

&nbsp;       otel.WithOTLPTransport(otel.OTLPTransportGRPC), // or OTLPTransportHTTP

&nbsp;       otel.WithOTLPBatching(100, 5\*time.Second),

&nbsp;       otel.WithOTLPCompression("gzip"),

&nbsp;       otel.WithOTLPClientCert("client.crt", "client.key"),

&nbsp;   ),

)



// Automatic trace context enrichment in HTTP handlers

func handleRequest(w http.ResponseWriter, r \*http.Request) {

&nbsp;   ctx := r.Context()

&nbsp;   logger := otel.NewRequestLogger(ctx,

&nbsp;       otel.WithOTLPEndpoint("localhost:4317"),

&nbsp;       otel.WithOTLPInsecure(),

&nbsp;   )

&nbsp;   

&nbsp;   // Logs automatically include trace.id, span.id, trace.flags

&nbsp;   logger.Information("Processing request for {Path}", r.URL.Path)

}



// Sampling strategies for high-volume scenarios

logger := mtlog.New(

&nbsp;   otel.WithOTLPSink(

&nbsp;       otel.WithOTLPEndpoint("localhost:4317"),

&nbsp;       otel.WithOTLPSampling(otel.NewRateSampler(0.1)), // Sample 10% of events

&nbsp;       // or: otel.NewLevelSampler(core.WarningLevel)    // Only warnings and above

&nbsp;       // or: otel.NewAdaptiveSampler(1000)              // Target 1000 events/sec

&nbsp;   ),

)



// Prometheus metrics export for monitoring

exporter, \_ := otel.NewMetricsExporter(

&nbsp;   otel.WithMetricsPort(9090),

&nbsp;   otel.WithMetricsPath("/metrics"),

)

defer exporter.Close()



// Use mtlog as an OTEL Bridge (mtlog -> OTEL)

otelLogger := otel.NewBridge(logger)

otelLogger.Emit(ctx, record) // Use OTEL log.Logger interface



// Use OTEL as an mtlog sink (OTEL -> mtlog)

handler := otel.NewHandler(otelLogger)

logger := mtlog.New(mtlog.WithSink(handler))

```



For complete OpenTelemetry integration documentation, see the \[OTEL adapter README](./adapters/otel/README.md).



\## Environment Variables



mtlog respects several environment variables for runtime configuration:



\### Color Control



```bash

\# Force specific color mode (overrides terminal detection)

export MTLOG\_FORCE\_COLOR=none     # Disable all colors

export MTLOG\_FORCE\_COLOR=8        # Force 8-color mode (basic ANSI)

export MTLOG\_FORCE\_COLOR=256      # Force 256-color mode



\# Standard NO\_COLOR variable is also respected

export NO\_COLOR=1                 # Disable colors (follows no-color.org)

```



\### Performance Tuning



```bash

\# Adjust source context cache size (default: 10000)

export MTLOG\_SOURCE\_CTX\_CACHE=50000  # Increase for large applications

export MTLOG\_SOURCE\_CTX\_CACHE=1000   # Decrease for memory-constrained environments



\# Adjust type name cache size (default: 10000)

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=50000  # For applications with many types

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=1000   # For memory-constrained environments

export MTLOG\_TYPE\_NAME\_CACHE\_SIZE=0      # Disable type name caching

```



\## Tools



\### mtlog-analyzer



A static analysis tool that catches common mtlog mistakes at compile time:



```bash

\# Install the analyzer

go install github.com/willibrandon/mtlog/cmd/mtlog-analyzer@latest



\# Run with go vet

go vet -vettool=$(which mtlog-analyzer) ./...

```



The analyzer detects:

\- Template/argument count mismatches

\- Invalid property names (spaces, starting with numbers)

\- Duplicate properties in templates and With() calls

\- Missing capturing hints for complex types

\- Error logging without error values

\- With() method issues (odd arguments, non-string keys, empty keys)

\- Cross-call duplicate detection for property overrides

\- Reserved property name shadowing (opt-in)



Example catches:

```go

// ❌ Template has 2 properties but 1 argument provided

log.Information("User {UserId} logged in from {IP}", userId)



// ❌ Duplicate property 'UserId'

log.Information("User {UserId} did {Action} as {UserId}", id, "login", id)



// ❌ With() requires even number of arguments (MTLOG009)

log.With("key1", "value1", "key2")  // Missing value



// ❌ With() key must be a string (MTLOG010)

log.With(123, "value")



// ❌ Duplicate keys in With() (MTLOG003)

log.With("id", 1, "name", "test", "id", 2)



// ⚠️ Cross-call property override (MTLOG011)

logger := log.With("service", "api")

logger.With("service", "auth")  // Overrides previous 'service'



// ✅ Correct usage

log.Information("User {@User} has {Count} items", user, count)

log.With("userId", 123, "requestId", "abc").Info("Request processed")

```



See \[mtlog-analyzer README](./cmd/mtlog-analyzer/README.md) for detailed documentation and CI integration.



\### mtlog-lsp



A Language Server Protocol implementation that bundles mtlog-analyzer for editor integrations:



```bash

\# Install the LSP server

go install github.com/willibrandon/mtlog/cmd/mtlog-lsp@latest

```



mtlog-lsp provides:

\- Zero-subprocess overhead with bundled analyzer

\- Real-time diagnostics for all MTLOG001-MTLOG013 issues

\- Code actions and quick fixes

\- Workspace configuration support

\- Performance optimized with package caching



Primarily used by the \[Zed extension](./zed-extension/mtlog/README.md). See \[mtlog-lsp README](./cmd/mtlog-lsp/README.md) for detailed documentation.



\### IDE Extensions



\#### VS Code Extension



For real-time validation in Visual Studio Code, install the \[mtlog-analyzer extension](./vscode-extension/mtlog-analyzer/README.md):



1\. Install mtlog-analyzer: `go install github.com/willibrandon/mtlog/cmd/mtlog-analyzer@latest`

2\. Install the extension from VS Code Marketplace (search for "mtlog-analyzer")

3\. Get instant feedback on template errors as you type



The extension provides:

\- 🔍 Real-time diagnostics with squiggly underlines

\- 🎯 Precise error locations - click to jump to issues

\- 📊 Three severity levels: errors, warnings, and suggestions

\- 🔧 Quick fixes for common issues (Ctrl+. for PascalCase conversion, argument count fixes)

\- ⚙️ Configurable analyzer path and flags



\#### GoLand Plugin



For real-time validation in GoLand and other JetBrains IDEs with Go support, install the \[mtlog-analyzer plugin](./goland-plugin/README.md):



1\. Install mtlog-analyzer: `go install github.com/willibrandon/mtlog/cmd/mtlog-analyzer@latest`

2\. Install the plugin from JetBrains Marketplace (search for "mtlog-analyzer")

3\. Get instant feedback on template errors as you type



The plugin provides:

\- 🔍 Real-time template validation as you type

\- 🎯 Intelligent highlighting (template errors highlight the full string, property warnings highlight just the property)

\- 🔧 Quick fixes for common issues (Alt+Enter for PascalCase conversion, argument count fixes)

\- ⚙️ Configurable analyzer path, flags, and severity levels

\- 🚀 Performance optimized with caching and debouncing



\#### Neovim Plugin



For Neovim users, a comprehensive plugin is included in the repository at \[neovim-plugin/](./neovim-plugin/):



```lua

-- Install with lazy.nvim

{

&nbsp; 'willibrandon/mtlog',

&nbsp; lazy = false,  -- Load immediately to ensure commands are available

&nbsp; config = function(plugin)

&nbsp;   -- Handle the plugin's subdirectory structure

&nbsp;   vim.opt.rtp:append(plugin.dir .. "/neovim-plugin")

&nbsp;   vim.cmd("runtime plugin/mtlog.vim")

&nbsp;   

&nbsp;   require('mtlog').setup()

&nbsp; end,

&nbsp; ft = 'go',

}

```



The plugin provides:

\- 🔍 Real-time analysis on save with debouncing

\- 🎯 LSP integration for code actions

\- 🔧 Quick fixes and diagnostic suppression

\- 📊 Statusline integration with diagnostic counts

\- ⚡ Advanced features: queue management, context rules, help system

\- 🚀 Performance optimized with caching and async operations



See the \[plugin README](./neovim-plugin/README.md) for detailed configuration and usage.



\#### Zed Extension



For real-time validation in Zed editor, install the \[mtlog-analyzer extension](./zed-extension/mtlog/README.md):



1\. Install mtlog-lsp (includes bundled analyzer):

&nbsp;  ```bash

&nbsp;  go install github.com/willibrandon/mtlog/cmd/mtlog-lsp@latest

&nbsp;  ```

2\. Install the extension from Zed's extension manager (search for "mtlog-analyzer")

3\. Get instant feedback on template errors as you type



The extension provides:

\- 🔍 Real-time diagnostics for all MTLOG001-MTLOG013 issues

\- 🔧 Quick fixes via code actions for common issues

\- 🚀 Automatic binary detection in standard Go paths

\- ⚙️ Configurable analyzer flags and custom paths



\## Advanced Usage



\### Custom Sinks



Implement the `core.LogEventSink` interface for custom outputs:



```go

type CustomSink struct{}



func (s \*CustomSink) Emit(event \*core.LogEvent) error {

&nbsp;   // Process the log event

&nbsp;   return nil

}



log := mtlog.New(

&nbsp;   mtlog.WithSink(\&CustomSink{}),

)

```



\### Custom Enrichers



Add custom properties to all events:



```go

type UserEnricher struct {

&nbsp;   userID int

}



func (e \*UserEnricher) Enrich(event \*core.LogEvent, factory core.LogEventPropertyFactory) {

&nbsp;   event.AddPropertyIfAbsent(factory.CreateProperty("UserId", e.userID))

}



log := mtlog.New(

&nbsp;   mtlog.WithEnricher(\&UserEnricher{userID: 123}),

)

```



\### Type Registration



Register types for special handling during capturing:



```go

capturer := capture.NewDefaultCapturer()

capturer.RegisterScalarType(reflect.TypeOf(uuid.UUID{}))

```



\## Documentation



For comprehensive guides and examples, see the \[docs](./docs) directory:



\- \*\*\[Quick Reference](./docs/quick-reference.md)\*\* - Quick reference for all features

\- \*\*\[Template Syntax](./docs/template-syntax.md)\*\* - Guide to message template syntaxes

\- \*\*\[Context Guide](./docs/context-guide.md)\*\* - Context logging, LogContext, and deadline awareness

\- \*\*\[Sampling Guide](./docs/sampling-guide.md)\*\* - Comprehensive per-message sampling documentation

\- \*\*\[Sinks Guide](./docs/sinks.md)\*\* - Complete guide to all output destinations

\- \*\*\[Routing Patterns](./docs/routing-patterns.md)\*\* - Advanced event routing patterns and best practices

\- \*\*\[Dynamic Level Control](./docs/dynamic-levels.md)\*\* - Runtime level management

\- \*\*\[Type-Safe Generics](./docs/generics.md)\*\* - Compile-time safe logging methods

\- \*\*\[Configuration](./docs/configuration.md)\*\* - JSON-based configuration

\- \*\*\[Performance](./docs/performance.md)\*\* - Benchmarks and optimization

\- \*\*\[Testing](./docs/testing.md)\*\* - Container-based integration testing

\- \*\*\[Troubleshooting](./docs/troubleshooting.md)\*\* - Debugging guide with selflog



\## Testing



```bash

\# Run unit tests

go test ./...



\# Run integration tests with Docker Compose

docker-compose -f docker/docker-compose.test.yml up -d

go test -tags=integration ./...

docker-compose -f docker/docker-compose.test.yml down



\# Run benchmarks (in benchmarks/ folder)

cd benchmarks \&\& go test -bench=. -benchmem

```



See \[testing.md](./docs/testing.md) for detailed testing guide and manual container setup.



\## Contributing



Contributions are welcome! Please see our \[Contributing Guide](CONTRIBUTING.md) for details.



\## License



This project is licensed under the MIT License - see the \[LICENSE](LICENSE) file for details.



