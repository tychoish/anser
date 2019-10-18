package anser

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/anser/mock"
	"github.com/mongodb/anser/model"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestSimpleMigrationGenerator(t *testing.T) {
	const jobTypeName = "simple-migration-generator"

	ctx := context.Background()
	assert := assert.New(t)
	env := mock.NewEnvironment()
	mh := &MigrationHelperMock{Environment: env}
	opts := model.GeneratorOptions{}
	ns := model.Namespace{DB: "foo", Collection: "bar"}

	assert.Implements((*Generator)(nil), &simpleMigrationGenerator{})

	// test that the factory produces a generator job
	factory, err := registry.GetJobFactory(jobTypeName)
	assert.NoError(err)
	job, ok := factory().(*simpleMigrationGenerator)
	assert.True(ok)
	assert.Equal(job.Type().Name, jobTypeName)

	// check that the public method produces a reasonable object
	// of the correct type, without shared state
	generator := NewSimpleMigrationGenerator(env, opts, map[string]interface{}{}).(*simpleMigrationGenerator)
	assert.NotNil(generator)
	assert.Equal(generator.Type().Name, jobTypeName)
	assert.NotEqual(generator, job)

	// check that the run method returns an error if it can't get a dependency error
	env.NetworkError = errors.New("injected network error")
	job.MigrationHelper = mh
	job.Run(ctx)
	assert.True(job.Status().Completed)
	if assert.True(job.HasErrors()) {
		err = job.Error()
		assert.Error(err)
		assert.Contains(err.Error(), "injected network error")
	}
	env.NetworkError = nil

	// make sure that session acquisition errors propagate
	job = factory().(*simpleMigrationGenerator)
	env.SessionError = errors.New("injected session error")
	job.MigrationHelper = mh
	job.Run(ctx)
	assert.True(job.Status().Completed)
	if assert.True(job.HasErrors()) {
		err = job.Error()
		assert.Error(err)
		assert.Contains(err.Error(), "injected session error")
	}
	env.SessionError = nil

	// make sure errors closing the iterator propagate
	job = factory().(*simpleMigrationGenerator)
	job.NS = ns
	job.MigrationHelper = mh
	env.Session = mock.NewSession()
	env.Session.DB("foo").C("bar").(*mock.LegacyCollection).QueryError = errors.New("query error")
	job.Run(ctx)
	assert.True(job.Status().Completed)
	if assert.True(job.HasErrors()) {
		err = job.Error()
		assert.Error(err)
		assert.Contains(err.Error(), "query error")
	}
	env.Session.DB("foo").C("bar").(*mock.LegacyCollection).QueryError = nil // reset

	// check job production
	job.Migrations = []*simpleMigrationJob{
		NewSimpleMigration(env, model.Simple{}).(*simpleMigrationJob),
		NewSimpleMigration(env, model.Simple{}).(*simpleMigrationJob),
		NewSimpleMigration(env, model.Simple{}).(*simpleMigrationJob),
	}
	counter := 0
	for migration := range job.Jobs() {
		assert.NotNil(migration)
		counter++
	}
	assert.Equal(counter, 3)

	// make sure that we generate the jobs we would expect to:
	job = factory().(*simpleMigrationGenerator)
	job.NS = ns
	job.MigrationHelper = mh
	job.Limit = 3
	job.SetID("simple")
	iter := &mock.Iterator{
		ShouldIter: true,
		Results: []interface{}{
			&doc{"one"}, &doc{"two"}, &doc{"three"}, &doc{"four"},
		},
	}

	ids := job.generateLegacyJobs(env, iter)
	for idx, id := range ids {
		assert.True(strings.HasPrefix(id, "simple."))
		assert.True(strings.HasSuffix(id, fmt.Sprintf(".%d", idx)))
		switch idx {
		case 0:
			assert.Contains(id, ".one.")
		case 1:
			assert.Contains(id, ".two.")
		case 2:
			assert.Contains(id, ".three.")
		}
	}

	assert.Len(ids, 3)
	assert.Len(job.Migrations, 3)

	network, err := env.GetDependencyNetwork()
	assert.NoError(err)
	network.AddGroup(job.ID(), ids)
	networkMap := network.Network()
	assert.Len(networkMap[job.ID()], 3)
}
