package newsblur

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gotest.tools/v3/assert"
)

const (
	envNewsblurUsername = "POSITRONIC_TEST_NEWSBLUR_USERNAME"
	envNewsblurPassword = "POSITRONIC_TEST_NEWSBLUR_PASSWORD"
)

func Test_Client(t *testing.T) {
	var (
		_          = uuid.New().String()
		nbUsername = os.Getenv(envNewsblurUsername)
		nbPassword = os.Getenv(envNewsblurPassword)
	)

	if nbUsername == "" || nbPassword == "" {
		t.Skipf("Skipping, missing %s or %s", envNewsblurUsername, envNewsblurPassword)
	}

	ctx := context.Background()
	cl, err := New(ctx, nbUsername, nbPassword)
	assert.NilError(t, err)

	_, err = cl.GetSharedStories(ctx, 1)
	assert.NilError(t, err)

	c, err := time.Parse(time.RFC3339, "2021-11-28T01:39:47.561Z")
	assert.NilError(t, err)

	//c = time.Now().Add(-1 * 12 * 30 * 24 * time.Hour)

	it, err := cl.SharedStoriesIterator(ctx, c)
	assert.NilError(t, err)

	for {
		story, err := it.Next(ctx)
		if err == io.EOF {
			break
		}
		assert.NilError(t, err)
		t.Log(story)
		t.Logf("Date: %s", story.SharedDate.Format(time.RFC3339Nano))
	}
}
