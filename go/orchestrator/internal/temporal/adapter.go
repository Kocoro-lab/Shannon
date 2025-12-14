package temporal

import (
	"fmt"
	"reflect"

	"go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

// ZapAdapter adapts zap logger to Temporal's logger interface
type ZapAdapter struct {
	logger *zap.Logger
}

func NewZapAdapter(logger *zap.Logger) log.Logger {
	return &ZapAdapter{logger: logger}
}

func (z *ZapAdapter) Debug(msg string, keyvals ...interface{}) {
	z.logger.Debug(msg, z.fieldsFromKeyvals(keyvals)...)
}

func (z *ZapAdapter) Info(msg string, keyvals ...interface{}) {
	z.logger.Info(msg, z.fieldsFromKeyvals(keyvals)...)
}

func (z *ZapAdapter) Warn(msg string, keyvals ...interface{}) {
	z.logger.Warn(msg, z.fieldsFromKeyvals(keyvals)...)
}

func (z *ZapAdapter) Error(msg string, keyvals ...interface{}) {
	z.logger.Error(msg, z.fieldsFromKeyvals(keyvals)...)
}

func (z *ZapAdapter) fieldsFromKeyvals(keyvals []interface{}) []zap.Field {
	fields := make([]zap.Field, 0, len(keyvals)/2)
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			key, ok := keyvals[i].(string)
			if ok {
				fields = append(fields, safeZapField(key, keyvals[i+1]))
			}
		}
	}
	return fields
}

// safeZapField creates a zap field, handling types that zap.Any() can't serialize
func safeZapField(key string, val interface{}) (field zap.Field) {
	// Recover from any panic during field creation
	defer func() {
		if r := recover(); r != nil {
			field = zap.String(key, fmt.Sprintf("<unserializable: %v>", r))
		}
	}()

	if val == nil {
		return zap.String(key, "<nil>")
	}

	// Check for types that can cause zap.Any() to panic
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Func:
		return zap.String(key, "<func>")
	case reflect.Chan:
		return zap.String(key, "<chan>")
	case reflect.UnsafePointer:
		return zap.String(key, "<unsafe.Pointer>")
	case reflect.Invalid:
		return zap.String(key, "<invalid>")
	default:
		return zap.Any(key, val)
	}
}

// With returns a new logger with additional fields - required for Temporal SDK compatibility
func (z *ZapAdapter) With(keyvals ...interface{}) log.Logger {
	return &ZapAdapter{logger: z.logger.With(z.fieldsFromKeyvals(keyvals)...)}
}

// safeString converts value to string safely for logging
func safeString(val interface{}) string {
	defer func() {
		recover() // Ignore any panic during string conversion
	}()
	return fmt.Sprintf("%v", val)
}
