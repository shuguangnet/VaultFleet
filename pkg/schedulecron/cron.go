package schedulecron

import (
	"strings"

	"github.com/robfig/cron/v3"
)

var parser = cron.NewParser(
	cron.Second |
		cron.Minute |
		cron.Hour |
		cron.Dom |
		cron.Month |
		cron.Dow |
		cron.Descriptor,
)

// Normalize converts standard five-field Cron to the six-field form consumed
// by the shared parser while leaving descriptors and legacy six-field Cron intact.
func Normalize(schedule string) string {
	schedule = strings.TrimSpace(schedule)
	if strings.HasPrefix(schedule, "@") {
		return schedule
	}
	if len(strings.Fields(schedule)) == 5 {
		return "0 " + schedule
	}
	return schedule
}

func Parse(schedule string) (cron.Schedule, error) {
	return parser.Parse(Normalize(schedule))
}

func Validate(schedule string) error {
	_, err := Parse(schedule)
	return err
}
