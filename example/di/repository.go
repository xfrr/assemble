package di

var _ Repository = (*InMemoryRepo)(nil)

// Repository is an example of a repository interface.
// Just for demonstration purposes.
type Repository interface {
	GetData(id string) (string, error)
}

// InMemoryRepo is a simple in-memory implementation of Repository.
// Just for demonstration purposes.
type InMemoryRepo struct {
}

func NewInMemoryRepo() (*InMemoryRepo, error) {
	return &InMemoryRepo{}, nil
}

func (r *InMemoryRepo) GetData(id string) (string, error) {
	return "data for " + id, nil
}
