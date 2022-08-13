package newsblur

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

const (
	envNewsblurUsername = "POSITRONIC_TEST_NEWSBLUR_USERNAME"
	envNewsblurPassword = "POSITRONIC_TEST_NEWSBLUR_PASSWORD"
)

func Test_Client(t *testing.T) {
	var (
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

	it, err := cl.SharedStoriesIterator(ctx, time.Now().Add(-1*3*30*24*time.Hour))
	assert.NilError(t, err)

	for {
		_, err := it.Next(ctx)
		if err == io.EOF {
			break
		}
		assert.NilError(t, err)
	}
}
