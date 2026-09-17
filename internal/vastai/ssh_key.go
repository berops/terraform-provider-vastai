package vastai

type CreateSSHKeyRequest struct {
	SSHKey string `json:"ssh_key"`
}

type CreateSSHKeyResponse struct {
	Success bool   `json:"success"`
	Key     SSHKey `json:"key"`
}

// UpdateSSHKeyRequest is the body of PUT /api/v0/ssh/{id}. The CLI sends the
// ID in the body as well as the path.
type UpdateSSHKeyRequest struct {
	ID     int64  `json:"id"`
	SSHKey string `json:"ssh_key"`
}

// UpdateSSHKeyResponse is the body returned by PUT /api/v0/ssh/{id}. The docs
// leave the shape of "key" unspecified, so it is decoded tolerantly.
type UpdateSSHKeyResponse struct {
	Success bool           `json:"success"`
	Key     sshKeyListItem `json:"key"`
}

// DeleteSSHKeyResponse is the body returned by DELETE /api/v0/ssh/{id}.
type DeleteSSHKeyResponse struct {
	Success bool `json:"success"`
}

// sshKeyListItem is one element of the GET /api/v0/ssh response. It is the
// same record as SSHKey, but the list endpoint documents the key field as
// "key" where the create endpoint returns "public_key". Both are accepted in
// case the two ever agree.
type sshKeyListItem struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"user_id"`
	Key       string  `json:"key"`
	PublicKey string  `json:"public_key"`
	CreatedAt string  `json:"created_at"`
	DeletedAt *string `json:"deleted_at"`
}

func (i sshKeyListItem) toSSHKey() SSHKey {
	publicKey := i.Key
	if publicKey == "" {
		publicKey = i.PublicKey
	}
	return SSHKey{
		ID:        i.ID,
		UserID:    i.UserID,
		PublicKey: publicKey,
		CreatedAt: i.CreatedAt,
		DeletedAt: i.DeletedAt,
	}
}

type SSHKey struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"user_id"`
	PublicKey string  `json:"public_key"`
	CreatedAt string  `json:"created_at"`
	DeletedAt *string `json:"deleted_at"`
}
