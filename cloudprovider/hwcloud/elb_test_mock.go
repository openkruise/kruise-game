package hwcloud

import (
	"context"

	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MockClient struct {
	mock.Mock
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) Status() client.SubResourceWriter {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) SubResource(subResource string) client.SubResourceClient {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) Scheme() *runtime.Scheme {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) RESTMapper() meta.RESTMapper {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}
