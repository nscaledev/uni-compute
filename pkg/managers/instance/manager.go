/*
Copyright 2024-2025 the Unikorn Authors.
Copyright 2026 Nscale.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package instance

import (
	"context"
	"slices"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/instance"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coremanager "github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/util"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Factory provides methods that can build a type specific controller.
type Factory struct{}

var _ coremanager.ControllerFactory = &Factory{}

// Metadata returns the application, version and revision.
func (*Factory) Metadata() util.ServiceDescriptor {
	return constants.ServiceDescriptor()
}

// Options returns any options to be added to the CLI flags and passed to the reconciler.
func (*Factory) Options() coremanager.ControllerOptions {
	return &instance.Options{}
}

// Reconciler returns a new reconciler instance.
func (*Factory) Reconciler(options *options.Options, controlerOptions coremanager.ControllerOptions, manager manager.Manager) reconcile.Reconciler {
	return coremanager.NewReconciler(options, controlerOptions, manager, instance.New)
}

// serverToInstanceMapFunc watches for server updates and triggers
// a reconsile of the instance.  This is done to react to things like status
// updates and deletions.
func serverToInstanceMapFunc(manager manager.Manager) func(context.Context, *regionv1.Server) []reconcile.Request {
	return func(ctx context.Context, server *regionv1.Server) []reconcile.Request {
		if server.Spec.Tags == nil {
			return nil
		}

		instanceID, ok := server.Spec.Tags.Find(constants.InstanceLabel)
		if !ok {
			return nil
		}

		// TODO: once we do away with project namespaces, this becomes a
		// direct lookup in our namespace.
		cli := manager.GetClient()

		var instances unikornv1.ComputeInstanceList

		if err := cli.List(ctx, &instances, &client.ListOptions{}); err != nil {
			return nil
		}

		predicate := func(instance unikornv1.ComputeInstance) bool {
			return instance.Name == instanceID
		}

		index := slices.IndexFunc(instances.Items, predicate)
		if index < 0 {
			return nil
		}

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: instances.Items[index].Namespace,
					Name:      instanceID,
				},
			},
		}
	}
}

// RegisterWatches adds any watches that would trigger a reconcile.
func (*Factory) RegisterWatches(manager manager.Manager, controller controller.Controller) error {
	// Any changes to the instance spec, trigger a reconcile.
	if err := controller.Watch(source.Kind(manager.GetCache(), &unikornv1.ComputeInstance{}, &handler.TypedEnqueueRequestForObject[*unikornv1.ComputeInstance]{}, &predicate.TypedGenerationChangedPredicate[*unikornv1.ComputeInstance]{})); err != nil {
		return err
	}

	// Any changes of servers trigger a reconsile of their owning instance.
	if err := controller.Watch(source.Kind(manager.GetCache(), &regionv1.Server{}, handler.TypedEnqueueRequestsFromMapFunc(serverToInstanceMapFunc(manager)), &predicate.TypedResourceVersionChangedPredicate[*regionv1.Server]{})); err != nil {
		return err
	}

	return nil
}

// Schemes allows controllers to add types to the client beyond
// the defaults defined in this repository.
func (*Factory) Schemes() []coreclient.SchemeAdder {
	return []coreclient.SchemeAdder{
		unikornv1.AddToScheme,
		regionv1.AddToScheme,
	}
}
