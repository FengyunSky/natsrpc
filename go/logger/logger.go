package logger

import log "github.com/sirupsen/logrus"

type Logger struct {
	module string
}

func (logger *Logger) Verbosef(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Debugf(tmp, args...)
}

func (logger *Logger) Infof(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Infof(tmp, args...)
}

func (logger *Logger) Warnf(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Warnf(tmp, args...)
}

func (logger *Logger) Errorf(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Errorf(tmp, args...)
}

func (logger *Logger) Panicf(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Panicf(tmp, args...)
}

func (logger *Logger) Fatalf(format string, args ...interface{}) {
	tmp := "[" + logger.module + "]" + format
	log.Fatalf(tmp, args...)
}

func NewLogger(module string) *Logger {
	return &Logger{
		module: module,
	}
}
