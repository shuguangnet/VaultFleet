package schedulecron

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSupportedFormats(t *testing.T) {
	for _, schedule := range []string{
		"30 2 * * *",
		"0 30 2 * * *",
		"@daily",
	} {
		t.Run(schedule, func(t *testing.T) {
			assert.NoError(t, Validate(schedule))
		})
	}
}

func TestValidateRejectsInvalidSchedule(t *testing.T) {
	assert.Error(t, Validate("not a cron expression"))
}
