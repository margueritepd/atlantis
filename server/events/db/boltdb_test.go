// Copyright 2017 HootSuite Media Inc.
//
// Licensed under the Apache License, Version 2.0 (the License);
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an AS IS BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Modified hereafter by contributors to runatlantis/atlantis.

package db_test

import (
	"github.com/runatlantis/atlantis/server/events/db"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
	"github.com/runatlantis/atlantis/server/events/models"
	. "github.com/runatlantis/atlantis/testing"
)

var lockBucket = "bucket"
var project = models.NewProject("owner/repo", "parent/child")
var workspace = "default"
var pullNum = 1
var lock = models.ProjectLock{
	Pull: models.PullRequest{
		Num: pullNum,
	},
	User: models.User{
		Username: "lkysow",
	},
	Workspace: workspace,
	Project:   project,
	Time:      time.Now(),
}

func TestListNoLocks(t *testing.T) {
	t.Log("listing locks when there are none should return an empty list")
	db, b := newTestDB()
	defer cleanupDB(db)
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))
}

func TestListOneLock(t *testing.T) {
	t.Log("listing locks when there is one should return it")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 1, len(ls))
}

func TestListMultipleLocks(t *testing.T) {
	t.Log("listing locks when there are multiple should return them")
	db, b := newTestDB()
	defer cleanupDB(db)

	// add multiple locks
	repos := []string{
		"owner/repo1",
		"owner/repo2",
		"owner/repo3",
		"owner/repo4",
	}

	for _, r := range repos {
		newLock := lock
		newLock.Project = models.NewProject(r, "path")
		_, _, err := b.TryLock(newLock)
		Ok(t, err)
	}
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 4, len(ls))
	for _, r := range repos {
		found := false
		for _, l := range ls {
			if l.Project.RepoFullName == r {
				found = true
			}
		}
		Assert(t, found, "expected %s in %v", r, ls)
	}
}

func TestListAddRemove(t *testing.T) {
	t.Log("listing after adding and removing should return none")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)
	_, err = b.Unlock(project, workspace)
	Ok(t, err)

	ls, err := b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))
}

func TestLockingNoLocks(t *testing.T) {
	t.Log("with no locks yet, lock should succeed")
	db, b := newTestDB()
	defer cleanupDB(db)
	acquired, currLock, err := b.TryLock(lock)
	Ok(t, err)
	Equals(t, true, acquired)
	Equals(t, lock, currLock)
}

func TestLockingExistingLock(t *testing.T) {
	t.Log("if there is an existing lock, lock should...")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)

	t.Log("...succeed if the new project has a different path")
	{
		newLock := lock
		newLock.Project = models.NewProject(project.RepoFullName, "different/path")
		acquired, currLock, err := b.TryLock(newLock)
		Ok(t, err)
		Equals(t, true, acquired)
		Equals(t, pullNum, currLock.Pull.Num)
	}

	t.Log("...succeed if the new project has a different workspace")
	{
		newLock := lock
		newLock.Workspace = "different-workspace"
		acquired, currLock, err := b.TryLock(newLock)
		Ok(t, err)
		Equals(t, true, acquired)
		Equals(t, newLock, currLock)
	}

	t.Log("...succeed if the new project has a different repoName")
	{
		newLock := lock
		newLock.Project = models.NewProject("different/repo", project.Path)
		acquired, currLock, err := b.TryLock(newLock)
		Ok(t, err)
		Equals(t, true, acquired)
		Equals(t, newLock, currLock)
	}

	t.Log("...not succeed if the new project only has a different pullNum")
	{
		newLock := lock
		newLock.Pull.Num = lock.Pull.Num + 1
		acquired, currLock, err := b.TryLock(newLock)
		Ok(t, err)
		Equals(t, false, acquired)
		Equals(t, currLock.Pull.Num, pullNum)
	}
}

func TestUnlockingNoLocks(t *testing.T) {
	t.Log("unlocking with no locks should succeed")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, err := b.Unlock(project, workspace)

	Ok(t, err)
}

func TestUnlocking(t *testing.T) {
	t.Log("unlocking with an existing lock should succeed")
	db, b := newTestDB()
	defer cleanupDB(db)

	_, _, err := b.TryLock(lock)
	Ok(t, err)
	_, err = b.Unlock(project, workspace)
	Ok(t, err)

	// should be no locks listed
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))

	// should be able to re-lock that repo with a new pull num
	newLock := lock
	newLock.Pull.Num = lock.Pull.Num + 1
	acquired, currLock, err := b.TryLock(newLock)
	Ok(t, err)
	Equals(t, true, acquired)
	Equals(t, newLock, currLock)
}

func TestUnlockingMultiple(t *testing.T) {
	t.Log("unlocking and locking multiple locks should succeed")
	db, b := newTestDB()
	defer cleanupDB(db)

	_, _, err := b.TryLock(lock)
	Ok(t, err)

	new := lock
	new.Project.RepoFullName = "new/repo"
	_, _, err = b.TryLock(new)
	Ok(t, err)

	new2 := lock
	new2.Project.Path = "new/path"
	_, _, err = b.TryLock(new2)
	Ok(t, err)

	new3 := lock
	new3.Workspace = "new-workspace"
	_, _, err = b.TryLock(new3)
	Ok(t, err)

	// now try and unlock them
	_, err = b.Unlock(new3.Project, new3.Workspace)
	Ok(t, err)
	_, err = b.Unlock(new2.Project, workspace)
	Ok(t, err)
	_, err = b.Unlock(new.Project, workspace)
	Ok(t, err)
	_, err = b.Unlock(project, workspace)
	Ok(t, err)

	// should be none left
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))
}

func TestUnlockByPullNone(t *testing.T) {
	t.Log("UnlockByPull should be successful when there are no locks")
	db, b := newTestDB()
	defer cleanupDB(db)

	_, err := b.UnlockByPull("any/repo", 1)
	Ok(t, err)
}

func TestUnlockByPullOne(t *testing.T) {
	t.Log("with one lock, UnlockByPull should...")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)

	t.Log("...delete nothing when its the same repo but a different pull")
	{
		_, err := b.UnlockByPull(project.RepoFullName, pullNum+1)
		Ok(t, err)
		ls, err := b.List()
		Ok(t, err)
		Equals(t, 1, len(ls))
	}
	t.Log("...delete nothing when its the same pull but a different repo")
	{
		_, err := b.UnlockByPull("different/repo", pullNum)
		Ok(t, err)
		ls, err := b.List()
		Ok(t, err)
		Equals(t, 1, len(ls))
	}
	t.Log("...delete the lock when its the same repo and pull")
	{
		_, err := b.UnlockByPull(project.RepoFullName, pullNum)
		Ok(t, err)
		ls, err := b.List()
		Ok(t, err)
		Equals(t, 0, len(ls))
	}
}

func TestUnlockByPullAfterUnlock(t *testing.T) {
	t.Log("after locking and unlocking, UnlockByPull should be successful")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)
	_, err = b.Unlock(project, workspace)
	Ok(t, err)

	_, err = b.UnlockByPull(project.RepoFullName, pullNum)
	Ok(t, err)
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))
}

func TestUnlockByPullMatching(t *testing.T) {
	t.Log("UnlockByPull should delete all locks in that repo and pull num")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)

	// add additional locks with the same repo and pull num but different paths/workspaces
	new := lock
	new.Project.Path = "dif/path"
	_, _, err = b.TryLock(new)
	Ok(t, err)
	new2 := lock
	new2.Workspace = "new-workspace"
	_, _, err = b.TryLock(new2)
	Ok(t, err)

	// there should now be 3
	ls, err := b.List()
	Ok(t, err)
	Equals(t, 3, len(ls))

	// should all be unlocked
	_, err = b.UnlockByPull(project.RepoFullName, pullNum)
	Ok(t, err)
	ls, err = b.List()
	Ok(t, err)
	Equals(t, 0, len(ls))
}

func TestGetLockNotThere(t *testing.T) {
	t.Log("getting a lock that doesn't exist should return a nil pointer")
	db, b := newTestDB()
	defer cleanupDB(db)
	l, err := b.GetLock(project, workspace)
	Ok(t, err)
	Equals(t, (*models.ProjectLock)(nil), l)
}

func TestGetLock(t *testing.T) {
	t.Log("getting a lock should return the lock")
	db, b := newTestDB()
	defer cleanupDB(db)
	_, _, err := b.TryLock(lock)
	Ok(t, err)

	l, err := b.GetLock(project, workspace)
	Ok(t, err)
	// can't compare against time so doing each field
	Equals(t, lock.Project, l.Project)
	Equals(t, lock.Workspace, l.Workspace)
	Equals(t, lock.Pull, l.Pull)
	Equals(t, lock.User, l.User)
}

// Test we can create a status and then get it.
func TestPullStatus_UpdateGet(t *testing.T) {
	b, cleanup := newTestDB2(t)
	defer cleanup()

	pull := models.PullRequest{
		Num:        1,
		HeadCommit: "sha",
		URL:        "url",
		HeadBranch: "head",
		BaseBranch: "base",
		Author:     "lkysow",
		State:      models.OpenPullState,
		BaseRepo: models.Repo{
			FullName:          "runatlantis/atlantis",
			Owner:             "runatlantis",
			Name:              "atlantis",
			CloneURL:          "clone-url",
			SanitizedCloneURL: "clone-url",
			VCSHost: models.VCSHost{
				Hostname: "github.com",
				Type:     models.Github,
			},
		},
	}
	status, err := b.UpdatePullWithResults(
		pull,
		[]models.ProjectResult{
			{
				RepoRelDir: ".",
				Workspace:  "default",
				Failure:    "failure",
			},
		})
	Ok(t, err)
	Assert(t, status != nil, "exp non-nil")

	status, err = b.GetPullStatus(pull)
	Ok(t, err)
	Equals(t, pull, status.Pull)
	Equals(t, []models.ProjectStatus{
		{
			Workspace:   "default",
			RepoRelDir:  ".",
			ProjectName: "",
			Status:      models.ErroredPlanStatus,
		},
	}, status.Projects)
}

// Test we can create a status, delete it, and then we shouldn't be able to get
// it.
func TestPullStatus_UpdateDeleteGet(t *testing.T) {
	b, cleanup := newTestDB2(t)
	defer cleanup()

	pull := models.PullRequest{
		Num:        1,
		HeadCommit: "sha",
		URL:        "url",
		HeadBranch: "head",
		BaseBranch: "base",
		Author:     "lkysow",
		State:      models.OpenPullState,
		BaseRepo: models.Repo{
			FullName:          "runatlantis/atlantis",
			Owner:             "runatlantis",
			Name:              "atlantis",
			CloneURL:          "clone-url",
			SanitizedCloneURL: "clone-url",
			VCSHost: models.VCSHost{
				Hostname: "github.com",
				Type:     models.Github,
			},
		},
	}
	status, err := b.UpdatePullWithResults(
		pull,
		[]models.ProjectResult{
			{
				RepoRelDir: ".",
				Workspace:  "default",
				Failure:    "failure",
			},
		})
	Ok(t, err)
	Assert(t, status != nil, "exp non-nil")

	err = b.DeletePullStatus(pull)
	Ok(t, err)

	status, err = b.GetPullStatus(pull)
	Ok(t, err)
	Assert(t, status == nil, "exp nil")
}

// Test we can create a status, delete a specific project's status within that
// pull status, and when we get all the project statuses, that specific project
// should not be there.
func TestPullStatus_UpdateDeleteProject(t *testing.T) {
	b, cleanup := newTestDB2(t)
	defer cleanup()

	pull := models.PullRequest{
		Num:        1,
		HeadCommit: "sha",
		URL:        "url",
		HeadBranch: "head",
		BaseBranch: "base",
		Author:     "lkysow",
		State:      models.OpenPullState,
		BaseRepo: models.Repo{
			FullName:          "runatlantis/atlantis",
			Owner:             "runatlantis",
			Name:              "atlantis",
			CloneURL:          "clone-url",
			SanitizedCloneURL: "clone-url",
			VCSHost: models.VCSHost{
				Hostname: "github.com",
				Type:     models.Github,
			},
		},
	}
	status, err := b.UpdatePullWithResults(
		pull,
		[]models.ProjectResult{
			{
				RepoRelDir: ".",
				Workspace:  "default",
				Failure:    "failure",
			},
			{
				RepoRelDir:   ".",
				Workspace:    "staging",
				ApplySuccess: "success!",
			},
		})
	Ok(t, err)
	Assert(t, status != nil, "exp non-nil")

	err = b.DeleteProjectStatus(pull, "default", ".")
	Ok(t, err)

	status, err = b.GetPullStatus(pull)
	Ok(t, err)
	Equals(t, pull, status.Pull)
	Equals(t, []models.ProjectStatus{
		{
			Workspace:   "staging",
			RepoRelDir:  ".",
			ProjectName: "",
			Status:      models.AppliedPlanStatus,
		},
	}, status.Projects)
}

// Test that if we update an existing pull status and our new status is for a
// different HeadSHA, that we just overwrite the old status.
func TestPullStatus_UpdateNewCommit(t *testing.T) {
	b, cleanup := newTestDB2(t)
	defer cleanup()

	pull := models.PullRequest{
		Num:        1,
		HeadCommit: "sha",
		URL:        "url",
		HeadBranch: "head",
		BaseBranch: "base",
		Author:     "lkysow",
		State:      models.OpenPullState,
		BaseRepo: models.Repo{
			FullName:          "runatlantis/atlantis",
			Owner:             "runatlantis",
			Name:              "atlantis",
			CloneURL:          "clone-url",
			SanitizedCloneURL: "clone-url",
			VCSHost: models.VCSHost{
				Hostname: "github.com",
				Type:     models.Github,
			},
		},
	}
	status, err := b.UpdatePullWithResults(
		pull,
		[]models.ProjectResult{
			{
				RepoRelDir: ".",
				Workspace:  "default",
				Failure:    "failure",
			},
		})
	Ok(t, err)
	Assert(t, status != nil, "exp non-nil")

	pull.HeadCommit = "newsha"
	status, err = b.UpdatePullWithResults(pull,
		[]models.ProjectResult{
			{
				RepoRelDir:   ".",
				Workspace:    "staging",
				ApplySuccess: "success!",
			},
		})

	Ok(t, err)
	Equals(t, 1, len(status.Projects))

	status, err = b.GetPullStatus(pull)
	Ok(t, err)
	Equals(t, pull, status.Pull)
	Equals(t, []models.ProjectStatus{
		{
			Workspace:   "staging",
			RepoRelDir:  ".",
			ProjectName: "",
			Status:      models.AppliedPlanStatus,
		},
	}, status.Projects)
}

// Test that if we update an existing pull status and our new status is for a
// the same commit, that we merge the statuses.
func TestPullStatus_UpdateMerge(t *testing.T) {
	b, cleanup := newTestDB2(t)
	defer cleanup()

	pull := models.PullRequest{
		Num:        1,
		HeadCommit: "sha",
		URL:        "url",
		HeadBranch: "head",
		BaseBranch: "base",
		Author:     "lkysow",
		State:      models.OpenPullState,
		BaseRepo: models.Repo{
			FullName:          "runatlantis/atlantis",
			Owner:             "runatlantis",
			Name:              "atlantis",
			CloneURL:          "clone-url",
			SanitizedCloneURL: "clone-url",
			VCSHost: models.VCSHost{
				Hostname: "github.com",
				Type:     models.Github,
			},
		},
	}
	status, err := b.UpdatePullWithResults(
		pull,
		[]models.ProjectResult{
			{
				RepoRelDir: "mergeme",
				Workspace:  "default",
				Failure:    "failure",
			},
			{
				RepoRelDir: "staythesame",
				Workspace:  "default",
				PlanSuccess: &models.PlanSuccess{
					TerraformOutput: "tf out",
					LockURL:         "lock-url",
					RePlanCmd:       "plan command",
					ApplyCmd:        "apply command",
				},
			},
		})
	Ok(t, err)
	Assert(t, status != nil, "exp non-nil")

	status, err = b.UpdatePullWithResults(pull,
		[]models.ProjectResult{
			{
				RepoRelDir:   "mergeme",
				Workspace:    "default",
				ApplySuccess: "applied!",
			},
			{
				RepoRelDir:   "newresult",
				Workspace:    "default",
				ApplySuccess: "success!",
			},
		})

	Ok(t, err)

	getStatus, err := b.GetPullStatus(pull)
	Ok(t, err)

	// Test both the pull state returned from the update call *and* the get
	// call.
	for _, s := range []*models.PullStatus{status, getStatus} {
		Equals(t, pull, s.Pull)
		Equals(t, []models.ProjectStatus{
			{
				RepoRelDir: "mergeme",
				Workspace:  "default",
				Status:     models.AppliedPlanStatus,
			},
			{
				RepoRelDir: "staythesame",
				Workspace:  "default",
				Status:     models.PlannedPlanStatus,
			},
			{
				RepoRelDir: "newresult",
				Workspace:  "default",
				Status:     models.AppliedPlanStatus,
			},
		}, status.Projects)
	}
}

// newTestDB returns a TestDB using a temporary path.
func newTestDB() (*bolt.DB, *db.BoltDB) {
	// Retrieve a temporary path.
	f, err := ioutil.TempFile("", "")
	if err != nil {
		panic(errors.Wrap(err, "failed to create temp file"))
	}
	path := f.Name()
	f.Close() // nolint: errcheck

	// Open the database.
	boltDB, err := bolt.Open(path, 0600, nil)
	if err != nil {
		panic(errors.Wrap(err, "could not start bolt DB"))
	}
	if err := boltDB.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(lockBucket)); err != nil {
			return errors.Wrap(err, "failed to create bucket")
		}
		return nil
	}); err != nil {
		panic(errors.Wrap(err, "could not create bucket"))
	}
	b, _ := db.NewWithDB(boltDB, lockBucket)
	return boltDB, b
}

func newTestDB2(t *testing.T) (*db.BoltDB, func()) {
	tmp, cleanup := TempDir(t)
	boltDB, err := db.New(tmp)
	Ok(t, err)
	return boltDB, func() {
		cleanup()
	}
}

func cleanupDB(db *bolt.DB) {
	os.Remove(db.Path()) // nolint: errcheck
	db.Close()           // nolint: errcheck
}