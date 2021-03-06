package authorization

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"errors"
	"testing"
	"time"

	"github.com/openshift/elasticsearch-proxy/pkg/handlers/clusterlogging/types"

	osprojectv1 "github.com/openshift/api/project/v1"
	"github.com/openshift/elasticsearch-proxy/pkg/clients"
	"github.com/openshift/elasticsearch-proxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	token = "ignored"
)

var _ = Describe("#evaluateRoles", func() {

	It("should only return allowed roles", func() {
		client := &mockOpenShiftClient{sarResponses: map[string]bool{
			"allowed":    true,
			"notallowed": false,
		}}
		backendRoles := map[string]config.BackendRoleConfig{
			"allowed": config.BackendRoleConfig{
				Namespace:        "anamespace",
				Verb:             "allowed",
				Resource:         "someresource",
				ResourceAPIGroup: "",
			},
			"notallowed": config.BackendRoleConfig{
				Namespace:        "anamespace",
				Verb:             "notallowed",
				Resource:         "someresource",
				ResourceAPIGroup: "",
			},
		}
		groups := []string{}
		roles := evaluateRoles(client, "auser", groups, backendRoles)
		Expect(roles).To(Equal(map[string]struct{}{"allowed": struct{}{}}))
	})

})

func TestRolesProjectsService(t *testing.T) {
	tests := []struct {
		client clients.OpenShiftClient
		roles  map[string]struct{}
		err    error
	}{
		{client: &mockOpenShiftClient{tokenReviewErr: errors.New("failed to get token")}, err: errors.New("failed to get token")},
		{client: &mockOpenShiftClient{}, roles: map[string]struct{}{"key": exists}},
		{client: &mockOpenShiftClient{subjectAccessErr: errors.New("review failed")}, roles: map[string]struct{}{}},
		{client: &mockOpenShiftClient{projectsErr: errors.New("projects failed")}, err: errors.New("projects failed")},
	}

	for _, test := range tests {
		s := NewRolesProjectsService(120, time.Nanosecond, map[string]config.BackendRoleConfig{"key": {}}, test.client)
		rolesAndProjects, err := s.getRolesAndProjects(token)
		if test.err == nil {
			require.Nil(t, err)
			assert.Equal(t, "jdoe", rolesAndProjects.review.UserName())
			assert.Equal(t, []string{"foo", "bar"}, rolesAndProjects.review.Groups())
			assert.Equal(t, test.roles, rolesAndProjects.roles)
			p := []types.Project{{Name: "myproject"}}
			assert.Equal(t, p, rolesAndProjects.projects)
		} else {
			assert.Equal(t, test.err, err)
		}
	}
}

func TestCacheExpiry(t *testing.T) {
	client := &mockOpenShiftClient{}
	duration := time.Millisecond * 50
	s := NewRolesProjectsService(120, duration, map[string]config.BackendRoleConfig{"key": {}}, client)
	s.getRolesAndProjects(token)
	assert.Equal(t, 1, client.tokenReviewCounter)
	s.getRolesAndProjects(token)
	assert.Equal(t, 1, client.tokenReviewCounter)
	time.Sleep(duration)
	s.getRolesAndProjects(token)
	assert.Equal(t, 2, client.tokenReviewCounter)
}

type mockOpenShiftClient struct {
	tokenReviewErr     error
	subjectAccessErr   error
	projectsErr        error
	tokenReviewCounter int
	sarResponses       map[string]bool
}

func (c *mockOpenShiftClient) TokenReview(token string) (*clients.TokenReview, error) {
	c.tokenReviewCounter++
	return &clients.TokenReview{&authenticationv1.TokenReview{
		Status: authenticationv1.TokenReviewStatus{User: authenticationv1.UserInfo{Username: "jdoe", Groups: []string{"foo", "bar"}}}},
	}, c.tokenReviewErr
}

func (c *mockOpenShiftClient) SubjectAccessReview(groups []string, user, namespace, verb, resource, apiGroup string) (bool, error) {
	if c.sarResponses != nil {
		if value, ok := c.sarResponses[verb]; ok {
			return value, nil
		}
	}
	return true, c.subjectAccessErr
}

func (c *mockOpenShiftClient) ListNamespaces(token string) ([]clients.Namespace, error) {
	return []clients.Namespace{{Ns: osprojectv1.Project{ObjectMeta: metav1.ObjectMeta{Name: "myproject"}}}}, c.projectsErr
}
