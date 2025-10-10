/*
Copyright 2024-2025 the Unikorn Authors.

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

package cluster

import (
	"context"
	"slices"
	"time"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coremanager "github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/util"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return &cluster.Options{}
}

// Reconciler returns a new reconciler instance.
func (*Factory) Reconciler(options *options.Options, controlerOptions coremanager.ControllerOptions, manager manager.Manager) reconcile.Reconciler {
	return coremanager.NewReconciler(options, controlerOptions, manager, cluster.New)
}

// serverEnqueueRequest watches for cluster server member updates and triggers
// a reconsile of the cluster.  This is done to react to things like status
// updates and deletions.
func serverToClusterMapFunc(manager manager.Manager) func(context.Context, *regionv1.Server) []reconcile.Request {
	return func(ctx context.Context, server *regionv1.Server) []reconcile.Request {
		if server.Spec.Tags == nil {
			return nil
		}

		clusterID, ok := server.Spec.Tags.Find(coreconstants.ComputeClusterLabel)
		if !ok {
			return nil
		}

		// TODO: once we do away with project namespaces, this becomes a
		// direct lookup in our namespace.
		cli := manager.GetClient()

		var clusters unikornv1.ComputeClusterList

		if err := cli.List(ctx, &clusters, &client.ListOptions{}); err != nil {
			return nil
		}

		predicate := func(cluster unikornv1.ComputeCluster) bool {
			return cluster.Name == clusterID
		}

		index := slices.IndexFunc(clusters.Items, predicate)
		if index < 0 {
			return nil
		}

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: clusters.Items[index].Namespace,
					Name:      clusterID,
				},
			},
		}
	}
}

// RegisterWatches adds any watches that would trigger a reconcile.
func (*Factory) RegisterWatches(manager manager.Manager, controller controller.Controller) error {
	// Any changes to the cluster spec, trigger a reconcile.
	if err := controller.Watch(source.Kind(manager.GetCache(), &unikornv1.ComputeCluster{}, &handler.TypedEnqueueRequestForObject[*unikornv1.ComputeCluster]{}, &predicate.TypedGenerationChangedPredicate[*unikornv1.ComputeCluster]{})); err != nil {
		return err
	}

	// Any changes of servers trigger a reconsile of their owning cluster.
	if err := controller.Watch(source.Kind(manager.GetCache(), &regionv1.Server{}, handler.TypedEnqueueRequestsFromMapFunc(serverToClusterMapFunc(manager)), &predicate.TypedResourceVersionChangedPredicate[*regionv1.Server]{})); err != nil {
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

func (*Factory) Upgrade(ctx context.Context, cli client.Client, options *options.Options) error {
	// Caches need to start!
	time.Sleep(5 * time.Second)

	// v1.9.0 moved all clusters into the controller namespace.
	// It also replaces the network annotation with a network label.
	clusters := &unikornv1.ComputeClusterList{}

	if err := cli.List(ctx, clusters); err != nil {
		return err
	}

	clusters.Items = slices.DeleteFunc(clusters.Items, func(cluster unikornv1.ComputeCluster) bool {
		return cluster.Namespace == options.Namespace
	})

	for i := range clusters.Items {
		cluster := &clusters.Items[i]

		// Update the labels and annotations to keep the validating admission policy happy for
		// both the creation and the update before deletion.
		cluster.Labels[coreconstants.NetworkLabel] = cluster.Annotations[coreconstants.PhysicalNetworkAnnotation]
		delete(cluster.Annotations, coreconstants.PhysicalNetworkAnnotation)

		// Migrate cluster to its new home.
		newCluster := cluster.DeepCopy()

		newCluster.ObjectMeta = metav1.ObjectMeta{
			Namespace:   options.Namespace,
			Name:        cluster.Name,
			Labels:      cluster.Labels,
			Annotations: cluster.Annotations,
		}

		if err := cli.Create(ctx, newCluster); err != nil {
			return err
		}

		// Delete the cluster directly.
		cluster.Finalizers = nil

		if err := cli.Update(ctx, cluster); err != nil {
			return err
		}

		if err := cli.Delete(ctx, cluster); err != nil {
			return err
		}
	}

	return nil
}
