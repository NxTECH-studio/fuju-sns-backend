package inmemory_test

import (
	"testing"

	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/internal/repository/testsupport"
)

// newContract returns a fresh, self-consistent set of in-memory
// repositories. The LinkStore is shared across Posts (writer) and the
// tag / image / ogp lookup paths, mirroring how cmd/server wires them.
func newContract(_ *testing.T) testsupport.Contract {
	links := inmemory.NewLinkStore()
	return testsupport.Contract{
		Users: inmemory.NewUserRepository(),
		Posts: inmemory.NewPostRepository(links),
		Likes: inmemory.NewLikeRepository(),
	}
}

func TestUserRepository_Contract(t *testing.T) {
	testsupport.RunUserRepositoryContract(t, newContract)
}

func TestPostRepository_Contract(t *testing.T) {
	testsupport.RunPostRepositoryContract(t, newContract)
}

func TestLikeRepository_Contract(t *testing.T) {
	testsupport.RunLikeRepositoryContract(t, newContract)
}
