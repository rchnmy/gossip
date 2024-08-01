package log

import (
    "os"
    "time"
    "path"
    "strconv"

    "github.com/rs/zerolog"
)

var (
    ver   string
    l, lc zerolog.Logger
)

const app = "gossip"

func init() {
    l = zerolog.New(os.Stderr).
        Level(zerolog.InfoLevel).
        With().
        Timestamp().
        Str("facility", app).
        Str("version", ver).
        Logger()

    lc = l.With().Caller().Logger().
        Sample(&zerolog.BurstSampler{
            Burst:  1,
            Period: 2 * time.Minute,
            NextSampler: &zerolog.BasicSampler{N: 8},
        })

    zerolog.LevelFieldName     = "severity"
    zerolog.TimestampFieldName = "@timestamp"
    zerolog.CallerMarshalFunc  = func(_ uintptr, f string, l int) string {
        return path.Base(f) + ":" + strconv.FormatInt(int64(l), 10)
    }
}

func Info() *zerolog.Event {
    return l.Info().Str("status", "ok")
}

func Err() *zerolog.Event {
    return lc.Error().Str("status", "err")
}

func Fatal() *zerolog.Event {
    return lc.Fatal().Str("status", "err")
}

