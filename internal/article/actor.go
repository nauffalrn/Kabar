package article

// Actor is the signed-in user performing a write.
type Actor struct {
	UserID   string
	AuthorID string
	Admin    bool
}

func (a *Actor) owns(authorID string) bool {
	return a != nil && (a.Admin || a.AuthorID == authorID)
}

func requireActor(a *Actor) error {
	if a == nil {
		return ErrUnauthenticated
	}
	return nil
}

func requireAdmin(a *Actor) error {
	if a == nil {
		return ErrUnauthenticated
	}
	if !a.Admin {
		return ErrForbidden
	}
	return nil
}
