package temporal

import (
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
				fields = append(fields, zap.Any(key, keyvals[i+1]))
			}
		}
	}
	return fields
}