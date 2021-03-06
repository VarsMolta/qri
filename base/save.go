package base

import (
	"context"
	"fmt"
	"io"

	"github.com/qri-io/dataset"
	"github.com/qri-io/ioes"
	"github.com/qri-io/qfs"
	"github.com/qri-io/qfs/cafs"
	"github.com/qri-io/qri/base/dsfs"
	"github.com/qri-io/qri/dsref"
	"github.com/qri-io/qri/logbook"
	"github.com/qri-io/qri/repo"
	"github.com/qri-io/qri/repo/profile"
	reporef "github.com/qri-io/qri/repo/ref"
	"github.com/qri-io/qri/startf"
)

// SaveSwitches is an alias for the switches that control how saves happen
type SaveSwitches = dsfs.SaveSwitches

// SaveDataset initializes a dataset from a dataset pointer and data file
func SaveDataset(ctx context.Context, r repo.Repo, str ioes.IOStreams, changes *dataset.Dataset, secrets map[string]string, scriptOut io.Writer, sw SaveSwitches) (ref reporef.DatasetRef, err error) {
	var (
		prevPath string
		pro      *profile.Profile
	)

	// TODO(dustmop): In a future change, move everything related to naming (from here until the
	// sw.DryRun check) up into a higher function. This function should take an initID instead,
	// and should not inspect changes.Peername or changes.Name at all.
	// All that logbook needs to create an initID is a dataset name, so naming must move up in
	// order to have accomplish this goal.

	if pro, err = r.Profile(); err != nil {
		return
	}
	peername := pro.Peername
	dsName := changes.Name

	inferredName := MaybeInferName(changes)
	if inferredName != "" {
		dsName = inferredName
	}

	prev, mutable, prevPath, err := PrepareHeadDatasetVersion(ctx, r, peername, dsName)
	if err != nil {
		log.Errorf("preparing dataset: %s", err)
		return
	}

	if prevPath != "" {
		log.Debugf("loading previous path: %s", prevPath)
		if sw.NewName && inferredName != "" {
			// Using --new flag, name was inferred, but it's already in use. Because the --new
			// flag was given, user is requesting we invent a unique name. Increment a counter
			// on the name until we find something that's available.
			dsName = GenerateAvailableName(r, peername, dsName)
			prev, mutable, prevPath, err = PrepareHeadDatasetVersion(ctx, r, peername, dsName)
			if err != nil {
				return
			}
		} else if sw.NewName {
			// Name was explicitly given, with the --new flag, but the name is already in use.
			// This is an error.
			// TODO(dlong): Add a test for this case.
			return ref, fmt.Errorf("dataset name has a previous version, cannot make new dataset")
		} else if inferredName != "" {
			// Name was inferred, and has previous version. Unclear if the user meant to create
			// a brand new dataset or if they wanted to add a new version to the existing dataset.
			// Raise an error recommending one of these course of actions.
			return ref, fmt.Errorf("inferred dataset name already exists. To add a new commit to this dataset, run save again with the dataset reference. To create a new dataset, use --new flag")
		}
	}

	// TODO(dustmop): In a future change, move code related to transform higher up, as we
	// untangle transform from save, and use apply instead.

	if sw.DryRun {
		str.PrintErr("🏃🏽‍♀️ dry run\n")

		// dry-runs store to an in-memory repo
		r, err = repo.NewMemRepo(pro, cafs.NewMapstore(), r.Filesystem(), profile.NewMemStore())
		if err != nil {
			log.Debugf("creating new memRepo: %s", err)
			return
		}
	}

	if changes.Transform != nil {
		// create a check func from a record of all the parts that the datasetPod is changing,
		// the startf package will use this function to ensure the same components aren't modified
		mutateCheck := startf.MutatedComponentsFunc(changes)

		opts := []func(*startf.ExecOpts){
			startf.AddQriRepo(r),
			startf.AddMutateFieldCheck(mutateCheck),
			startf.SetErrWriter(scriptOut),
			startf.SetSecrets(secrets),
		}

		if err = startf.ExecScript(ctx, changes, prev, opts...); err != nil {
			return
		}

		str.PrintErr("✅ transform complete\n")
	}

	if prevPath == "" && changes.BodyFile() == nil && changes.Structure == nil {
		err = fmt.Errorf("creating a new dataset requires a structure or a body")
		return
	}

	if changes.BodyFile() != nil && prev.Structure != nil && changes.Structure != nil && prev.Structure.Format != changes.Structure.Format {
		if sw.ConvertFormatToPrev {
			var f qfs.File
			f, err = ConvertBodyFormat(changes.BodyFile(), changes.Structure, prev.Structure)
			if err != nil {
				return
			}
			// Set the new format on the change structure.
			changes.Structure.Format = prev.Structure.Format
			changes.SetBodyFile(f)
		} else {
			err = fmt.Errorf("Refusing to change structure from %s to %s",
				prev.Structure.Format, changes.Structure.Format)
			return
		}
	}

	if !sw.Replace {
		// Treat the changes as a set of patches applied to the previous dataset
		mutable.Assign(changes)
		changes = mutable
	}

	// infer missing values
	if err = InferValues(pro, changes); err != nil {
		return
	}

	// let's make history, if it exists
	changes.PreviousPath = prevPath

	// TODO(dustmop): Remove the need to assign this. See inside of CreateDataset for details
	changes.Name = dsName
	return CreateDataset(ctx, r, str, changes, prev, sw)
}

// CreateDataset uses dsfs to add a dataset to a repo's store, updating all
// references within the repo if successful
func CreateDataset(ctx context.Context, r repo.Repo, streams ioes.IOStreams, ds, dsPrev *dataset.Dataset, sw SaveSwitches) (ref reporef.DatasetRef, err error) {
	var (
		pro     *profile.Profile
		path    string
		resBody qfs.File
	)

	pro, err = r.Profile()
	if err != nil {
		log.Debugf("getting repo profile: %s", err)
		return
	}
	// TODO(dustmop): Remove the dependence on the ds having an assigned Name. It is only
	// needed for updating the refstore. Either pass in the reference needed to update the refstore,
	// or move the refstore update out of this function.
	dsName := ds.Name
	if dsName == "" {
		return ref, fmt.Errorf("cannot create dataset without a name")
	}
	if err = Drop(ds, sw.Drop); err != nil {
		log.Debugf("dropping components: %s", err)
		return ref, err
	}

	// TODO(dustmop): ValidateDataset relies upon having ds.Name set. Remove that assumption.
	if err = ValidateDataset(ds); err != nil {
		log.Debugf("ValidateDataset: %s", err)
		return
	}

	if path, err = dsfs.CreateDataset(ctx, r.Store(), ds, dsPrev, r.PrivateKey(), sw); err != nil {
		log.Debugf("dsfs.CreateDataset: %s", err)
		return
	}
	if ds.PreviousPath != "" && ds.PreviousPath != "/" {
		prev := reporef.DatasetRef{
			ProfileID: pro.ID,
			Peername:  pro.Peername,
			Name:      dsName,
			Path:      ds.PreviousPath,
		}

		// should be ok to skip this error. we may not have the previous
		// reference locally
		_ = r.DeleteRef(prev)
	}

	// TODO(dustmop): Reference is created here in order to update refstore. As we move to initID
	// and dscache, this will no longer be necessary, updating logbook will be enough.
	ref = reporef.DatasetRef{
		ProfileID: pro.ID,
		Peername:  pro.Peername,
		Name:      dsName,
		Path:      path,
	}

	if !sw.DryRun {
		if err = r.PutRef(ref); err != nil {
			log.Debugf("r.PutRef: %s", err)
			return
		}

		// TODO(dustmop): When we switch to initIDs, use the initID passed to this function,
		// retrieved from the top-level resolver.
		// Whether there is a previous version is equivalent to whether we have an initID coming
		// into this function.
		initID, err := r.Logbook().RefToInitID(dsref.Ref{Username: pro.Peername, Name: dsName})
		if err == logbook.ErrNotFound {
			// If dataset does not exist yet, initialize with the given name
			initID, err = r.Logbook().WriteDatasetInit(ctx, dsName)
			if err != nil {
				return ref, err
			}
		}
		// TODO(dustmop): Not checking the error return from RefToInitID. That function should
		// return an error if the dataset name is an empty string, but this breaks lots of tests,
		// because many tests rely on sending datasets with empty names.

		err = r.Logbook().WriteVersionSave(ctx, initID, ds)
		if err != nil && err != logbook.ErrNoLogbook {
			return ref, err
		}
	}

	ds, err = dsfs.LoadDataset(ctx, r.Store(), ref.Path)
	if err != nil {
		return ref, err
	}
	ds.ProfileID = pro.ID.String()
	ds.Name = ref.Name
	ds.Peername = ref.Peername
	ds.Path = path
	ref.Dataset = ds

	// need to open here b/c we might be doing a dry-run, which would mean we have
	// references to files in a store that won't exist after this function call
	// TODO (b5): this should be replaced with a call to OpenDataset with a qfs that
	// knows about the store
	if resBody, err = r.Store().Get(ctx, ref.Dataset.BodyPath); err != nil {
		log.Error("error getting from store:", err.Error())
	}
	ref.Dataset.SetBodyFile(resBody)
	return
}

// GenerateAvailableName creates a name for the dataset that is not currently in use
func GenerateAvailableName(r repo.Repo, peername, prefix string) string {
	counter := 0
	for {
		counter++
		tryName := fmt.Sprintf("%s_%d", prefix, counter)
		lookup := &reporef.DatasetRef{Name: tryName, Peername: peername}
		err := repo.CanonicalizeDatasetRef(r, lookup)
		if err == repo.ErrNotFound {
			return tryName
		}
	}
}
