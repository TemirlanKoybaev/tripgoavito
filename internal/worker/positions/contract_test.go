package positions

import "github.com/TemirlanKoybaev/tripgoavito.git/internal/gateway/push"

var _ pushGateway = (*push.Client)(nil)
