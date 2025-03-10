// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package migrator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/erda-project/erda/pkg/database/sqlparser/ddlreverser"
	"github.com/erda-project/erda/pkg/database/sqlparser/pygrator"
	"github.com/erda-project/erda/pkg/database/sqlparser/snapshot"
)

// installing type
const (
	firstTimeInstall installType = "first_time_install"
	normalUpdate     installType = "normal_update"
	firstTimeUpdate  installType = "first_time_update"

	patchPrefix = "patch-"
)

type installType string

const patchesKey = "patchesKey"

type Migrator struct {
	Parameters

	snap         *snapshot.Snapshot
	LocalScripts *Scripts
	reversing    []string

	installingType installType

	dbSettings      *pygrator.Settings // database settings
	sandboxSettings *pygrator.Settings // sandbox settings
	db              *gorm.DB
	sandbox         *gorm.DB
}

func New(parameters Parameters) (mig *Migrator, err error) {
	if parameters == nil {
		return nil, errors.New("parameters is nil")
	}
	if parameters.MySQLParameters() == nil {
		return nil, errors.New("MySQL DSN is invalid, did you set the right DSN ?")
	}
	if parameters.SandboxParameters() == nil {
		return nil, errors.New("sandbox DSN is invalid, did you set the right DSN ?")
	}

	// init parameters
	mig = new(Migrator)
	mig.Parameters = parameters
	mig.dbSettings = &pygrator.Settings{
		Engine:   pygrator.DjangoMySQLEngine,
		User:     mig.MySQLParameters().Username,
		Password: mig.MySQLParameters().Password,
		Host:     mig.MySQLParameters().Host,
		Port:     mig.MySQLParameters().Port,
		Name:     mig.MySQLParameters().Database,
		TimeZone: pygrator.TimeZoneAsiaShanghai,
	}
	mig.sandboxSettings = &pygrator.Settings{
		Engine:   pygrator.DjangoMySQLEngine,
		User:     mig.SandboxParameters().Username,
		Password: mig.SandboxParameters().Password,
		Host:     mig.SandboxParameters().Host,
		Port:     mig.SandboxParameters().Port,
		Name:     mig.SandboxParameters().Database,
		TimeZone: pygrator.TimeZoneAsiaShanghai,
	}

	// load scripts
	mig.LocalScripts, err = NewScripts(parameters)
	if err != nil {
		return nil, errors.Wrap(err, "failed to NewScripts")
	}

	return mig, nil
}

func (mig *Migrator) Run() (err error) {
	defer func() {
		if err == nil {
			logrus.Infoln("Erda MySQL Migrate Complete !")
		}
	}()

	// snapshot database schema structure
	mig.snap, err = snapshot.From(mig.DB(), SchemaMigrationHistory)
	if err != nil {
		return err
	}

	// if there is no table then goto new installation
	if !mig.snap.HasAnyTable() {
		logrus.Infoln("there is not any table, it is the first time installation...")
		mig.installingType = firstTimeInstall
		return mig.newInstallation()
	}

	// if there is any history record then goto normal update,
	// otherwise goto first time update
	var histories []HistoryModel
	tx := mig.DB().Find(&histories)
	if tx.Error == nil && len(histories) > 0 {
		logrus.Infoln("found migration histories, it is the normal update installation...")
		mig.installingType = normalUpdate
	} else {
		logrus.Infoln("there are some tables but no migration histories, it is the first time update installation by Erda MySQL Migration...")
		mig.installingType = firstTimeUpdate
	}
	return mig.normalUpdate()
}

func (mig *Migrator) newInstallation() (err error) {
	new(HistoryModel).CreateTable(mig.DB())
	mig.LocalScripts.MarkPending(mig.DB())

	// Erda mysql lint
	if !mig.SkipMigrationLint() {
		logrus.Infoln("DO ERDA MYSQL LINT...")
		if err = mig.LocalScripts.Lint(); err != nil {
			return err
		}
		logrus.Infoln("OK")
	}

	// same name lint
	logrus.Infoln("DO SAME NAME LINT....")
	if err = mig.LocalScripts.SameNameLint(); err != nil {
		return err
	}
	logrus.Infoln("OK")

	// alter permission lint
	logrus.Infoln("DO ALTER PERMISSION LINT...")
	if err = mig.LocalScripts.AlterPermissionLint(); err != nil {
		return err
	}
	logrus.Infoln("OK")

	var ctx = context.Background()
	// execute in sandbox
	if !mig.SkipSandbox() {
		logrus.Infoln("DO MIGRATION IN SANDBOX...")
		if err = mig.migrateSandbox(ctx); err != nil {
			return err
		}
		logrus.Infoln("OK")
	}

	// pre-migrate schema SQLs
	if !mig.SkipPreMigrate() && !mig.SkipMigrate() {
		//logrus.Infoln("DO PRE-MIGRATION...")
		//if err = mig.preMigrate(ctx); err != nil {
		//	return err
		//}
		//logrus.Infoln("OK")
	}

	// migrate
	if !mig.SkipMigrate() {
		logrus.Infoln("DO MIGRATION...")
		if err = mig.migrate(ctx); err != nil {
			return err
		}
		logrus.Infoln("MIGRATE OK")
	}

	return nil
}

func (mig *Migrator) normalUpdate() (err error) {
	mig.LocalScripts.MarkPending(mig.DB())

	// Erda mysql lint
	if !mig.SkipMigrationLint() {
		logrus.Infoln("DO ERDA MYSQL LINT....")
		if err = mig.LocalScripts.Lint(); err != nil {
			return err
		}
		logrus.Infoln("OK")
	}

	// same name lint
	logrus.Infoln("DO SAME NAME LINT....")
	if err = mig.LocalScripts.SameNameLint(); err != nil {
		return err
	}
	logrus.Infoln("OK")

	// alter permission lint
	logrus.Infoln("DO ALTER PERMISSION LINT....")
	if err = mig.LocalScripts.AlterPermissionLint(); err != nil {
		return err
	}
	logrus.Infoln("OK")

	ctx := context.Background()

	// installed script changes lint
	logrus.Infoln("DO INSTALLED CHANGES LINT....")
	if err = mig.LocalScripts.InstalledChangesLint(&ctx, mig.DB()); err != nil {
		return err
	}
	logrus.Infoln("OK")

	if !mig.SkipSandbox() {
		// migrate in sandbox
		logrus.Infoln("DO MIGRATION IN SANDBOX....")
		if err = mig.migrateSandbox(ctx); err != nil {
			return err
		}
		logrus.Infoln("OK")
	}

	// pre migrate data
	if !mig.SkipPreMigrate() && !mig.SkipMigrate() {
		//logrus.Infoln("DO PRE-MIGRATION....")
		//if err = mig.preMigrate(ctx); err != nil {
		//	return err
		//}
		//logrus.Infoln("PRE-MIGRATE OK")
	}

	// migrate data
	if !mig.SkipMigrate() {
		logrus.Infoln("DO MIGRATION....")
		if err = mig.migrate(ctx); err != nil {
			return err
		}
		logrus.Infoln("MIGRATE OK")
	}

	return nil
}

func (mig *Migrator) reverse(reversing []string, reverseSlice bool) error {
	if len(reversing) == 0 {
		return nil
	}
	if reverseSlice {
		ddlreverser.ReverseSlice(reversing)
	}
	for _, s := range mig.reversing {
		if err := mig.DB().Exec(s).Error; err != nil {
			return errors.Errorf("failed to exec reversing SQL: %s", s)
		}
	}
	return nil
}

func (mig *Migrator) migrateSandbox(ctx context.Context) (err error) {
	if err = mig.destructiveLint(); err != nil {
		return err
	}

	snap, err := snapshot.From(mig.DB())
	if err != nil {
		return err
	}
	// copy database snapshot to sandbox
	logrus.Infoln("copy current database structure to sandbox")
	if err = snap.RecoverTo(mig.SandBox()); err != nil {
		return err
	}
	logrus.Infoln("ok")

	rows, err := mig.SandBox().Raw("show tables").Rows()
	if err != nil {
		return err
	}
	logrus.Infoln("tables in sandbox:")
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return errors.Wrapf(err, "scan error")
		}
		logrus.Infoln(tableName)
	}

	if mig.installingType == firstTimeUpdate {
		reason, ok := mig.compareSchemas(mig.SandBox())
		if !ok {
			logrus.Warnf("local schema is not equal with cloud schema, try to resolve it:\n%s", reason)
			if err := mig.patchBeforeMigrating(mig.SandBox(), []string{patchInit}); err != nil {
				return errors.Wrap(err, "failed to patch init")
			}
			reason, ok := mig.compareSchemas(mig.SandBox())
			if !ok {
				return errors.Errorf("local base schema is not equal with cloud schema:\n%s", reason)
			}
		}

		// record base
		logrus.Infoln("RECORD BASE... ..")
		new(HistoryModel).CreateTable(mig.SandBox())
		now := time.Now()
		for moduleName, module := range mig.LocalScripts.Services {
			for i := range module.Scripts {
				module.Scripts[i].Pending = true
				if module.Scripts[i].IsBaseline() {
					module.Scripts[i].Pending = false
					record := HistoryModel{
						ID:           0,
						CreatedAt:    now,
						UpdatedAt:    now,
						ServiceName:  moduleName,
						Filename:     filepath.Base(module.Scripts[i].GetName()),
						Checksum:     module.Scripts[i].Checksum(),
						InstalledBy:  "",
						InstalledOn:  "",
						LanguageType: string(module.Scripts[i].Type),
						Reversed:     ddlreverser.ReverseCreateTableStmts(module.Scripts[i]),
					}
					if err := mig.SandBox().Create(&record).Error; err != nil {
						return err
					}
				}
			}
		}
	}

	files, err := retrievePatchesFiles(ctx)
	if err != nil {
		return errors.New("failed to retrieve patches files")
	}
	logrus.Infoln("retrieve files that needs to patch:", files)
	if err = mig.patchBeforeMigrating(mig.SandBox(), files); err != nil {
		return errors.Wrapf(err, "failed to patch before migrating in sandbox")
	}

	// install every module
	for moduleName, module := range mig.LocalScripts.Services {
		for _, script := range module.Scripts {
			if !script.Pending {
				continue
			}
			switch script.Type {
			case ScriptTypeSQL:
				after := func(tx *gorm.DB, err error) {
					tx.Commit()
				}
				tx := mig.SandBox().Begin()
				if err := mig.installSQL(script, mig.SandBox(), tx, after); err != nil {
					return errors.Wrapf(err, "failed to migrate in sandbox:  module name: %s, filename: %s, type: %s",
						moduleName, script.GetName(), ScriptTypeSQL)
				}
				if err := mig.patchSQLAfterInstalling(script, mig.SandBox()); err != nil {
					return errors.Wrapf(err, "failed to migrate patch in sandbox, module name: %s, filename: %s, type: %s",
						moduleName, script.GetName(), ScriptTypeSQL)
				}
			case ScriptTypePython:
				if err := mig.installPy(script, module, mig.sandboxSettings, true); err != nil {
					return errors.Wrapf(err, "failed to migrate in sandbox: %+v",
						map[string]interface{}{"moduleName": moduleName, "filename": script.GetName(), "type": ScriptTypePython})
				}
			}
		}
	}

	return err
}

// pre migrate data SQLs, all applied in this runtime will be rollback
func (mig *Migrator) preMigrate(ctx context.Context) (err error) {
	if err = mig.destructiveLint(); err != nil {
		return err
	}

	// finally roll all DDL back
	defer func() {
		logrus.Infoln("	REVERSE PRE-MIGRATIONS: all schema migration")
		if err := mig.reverse(mig.reversing, true); err != nil {
			logrus.Fatalln(err)
		}
	}()

	mig.reversing = nil

	files, err := retrievePatchesFiles(ctx)
	if err != nil {
		return err
	}
	if err = mig.patchBeforeMigrating(mig.DB(), files); err != nil {
		return errors.Wrapf(err, "failed to patch before migrating in pre-migration")
	}

	// install every module
	for moduleName, module := range mig.LocalScripts.Services {
		for _, script := range module.Scripts {
			if !script.Pending {
				continue
			}

			switch script.Type {
			case ScriptTypeSQL:
				after := func(tx *gorm.DB, err error) {
					logrus.WithField("module name", moduleName).
						WithField("script name", script.GetName()).
						Infoln("	ROLLBACK PRE-MIGRATIONS: current script data migration")
					tx.Rollback()
				}
				tx := mig.DB().Begin()
				if err := mig.installSQL(script, mig.DB(), tx, after); err != nil {
					return errors.Wrapf(err, "failed to pre-migrate, module name: %s, script name: %s, type: %s",
						moduleName, script.GetName(), ScriptTypeSQL)
				}
				if err := mig.patchSQLAfterInstalling(script, mig.DB()); err != nil {
					return errors.Wrapf(err, "failed to patch after pre-migreating, module name: %s, script name: %s, type: %s",
						moduleName, script.GetName(), ScriptTypeSQL)
				}
			case ScriptTypePython:
				if err := mig.installPy(script, module, mig.dbSettings, false); err != nil {
					return errors.Wrapf(err, "failed to pre-migrate: %+v",
						map[string]interface{}{"module name": moduleName, "script name": script.GetName(), "type": ScriptTypePython})
				}
			}
		}
	}

	return nil
}

func (mig *Migrator) migrate(ctx context.Context) error {
	now := time.Now()

	if mig.installingType == firstTimeUpdate {
		_, ok := mig.compareSchemas(mig.DB())
		if !ok {
			if err := mig.patchBeforeMigrating(mig.DB(), []string{patchInit}); err != nil {
				return errors.Wrap(err, "failed to patch init")
			}
			reason, ok := mig.compareSchemas(mig.DB())
			if !ok {
				return errors.Errorf("local base schema is not equal with cloud schema: %s", reason)
			}
		}

		// record base
		logrus.Infoln("RECORD BASE... ..")
		new(HistoryModel).CreateTable(mig.DB())
		now := time.Now()
		for moduleName, module := range mig.LocalScripts.Services {
			for i := range module.Scripts {
				module.Scripts[i].Pending = true
				if module.Scripts[i].IsBaseline() {
					module.Scripts[i].Pending = false
					record := HistoryModel{
						ID:           0,
						CreatedAt:    now,
						UpdatedAt:    now,
						ServiceName:  moduleName,
						Filename:     filepath.Base(module.Scripts[i].GetName()),
						Checksum:     module.Scripts[i].Checksum(),
						InstalledBy:  "",
						InstalledOn:  "",
						LanguageType: string(module.Scripts[i].Type),
						Reversed:     ddlreverser.ReverseCreateTableStmts(module.Scripts[i]),
					}
					if err := mig.DB().Create(&record).Error; err != nil {
						return err
					}
				}
			}
		}
	}

	files, err := retrievePatchesFiles(ctx)
	if err != nil {
		return err
	}
	logrus.Infoln("retrieve files that needs to patch:", files)
	if err := mig.patchBeforeMigrating(mig.DB(), files); err != nil {
		return errors.Wrap(err, "failed to patch before migrating")
	}

	// install every service
	for moduleName, mod := range mig.LocalScripts.Services {
		logrus.WithField("module", moduleName).Infoln()
		for _, script := range mod.Scripts {
			if !script.Pending {
				continue
			}

			logrus.WithField("script name", script.GetName()).Infoln("install")
			switch script.Type {
			case ScriptTypeSQL:
				after := func(tx *gorm.DB, err error) {
					if err != nil {
						tx.Rollback()
						mig.reverse(script.Reversing, true)
					} else {
						tx.Commit()
					}
				}
				tx := mig.DB().Begin()
				if err := mig.installSQL(script, mig.DB(), tx, after); err != nil {
					return errors.Wrapf(err, "failed to migrate: %+v",
						map[string]interface{}{"module name": moduleName, "script name": script.GetName(), "type": ScriptTypeSQL})
				}
				if err := mig.patchSQLAfterInstalling(script, mig.DB()); err != nil {
					return errors.Wrapf(err, "failed to patch after migrating, module name: %s, script name: %s, type: %s",
						moduleName, script.GetName(), ScriptTypeSQL)
				}

			case ScriptTypePython:
				if err := mig.installPy(script, mod, mig.dbSettings, true); err != nil {
					return errors.Wrapf(err, "failed to migrate: %+v",
						map[string]interface{}{"module name": moduleName, "script name": script.GetName(), "type": ScriptTypePython})
				}
			}

			// record it
			ddlreverser.ReverseSlice(script.Reversing)
			record := HistoryModel{
				ID:           0,
				CreatedAt:    now,
				UpdatedAt:    now,
				ServiceName:  moduleName,
				Filename:     script.GetName(),
				Checksum:     script.Checksum(),
				InstalledBy:  "",
				InstalledOn:  "",
				LanguageType: string(script.Type),
				Reversed:     strings.Join(script.Reversing, "\n"),
			}
			if err := mig.DB().Create(&record).Error; err != nil {
				return errors.Wrapf(err, "internal error: failed to record migration")
			}
		}
	}

	return nil
}

func (mig *Migrator) patchBeforeMigrating(db *gorm.DB, files []string) error {
	mod := mig.LocalScripts.GetPatches()
	if mod == nil {
		return nil
	}

	logrus.WithField("module", mod.Name)
	for _, script := range mod.Scripts {
		var (
			record   HistoryModel
			filename = strings.TrimPrefix(script.GetName(), patchPrefix)
			where    = map[string]interface{}{"filename": filename}
		)
		// if the script is not in the diff checksum file list, skip
		var in = false
		for _, file := range files {
			if file == script.GetName() {
				in = true
				break
			}
		}
		if !in {
			continue
		}

		// if the file is going to be patched is not installed, do not patch it
		if db := db.Where(where).First(&record); (db.Error != nil || db.RowsAffected == 0) && script.GetName() != patchInit {
			continue
		}

		switch script.Type {
		case ScriptTypeSQL:
			logrus.WithField("script name", script.GetName()).Infoln("patch it before all migrating")
			logrus.Infof("script Rawtext: %s", string(script.Rawtext))
			if err := db.Exec(string(script.Rawtext)).Error; err != nil {
				return errors.Wrapf(err, "failed to patch, module name: %s, script name: %s, type: %s",
					mod.Name, script.GetName(), ScriptTypeSQL)
			}

			// correct the checksum
			// if there is no corresponding original file, skip
			_, originalScript, ok := mig.LocalScripts.GetScriptByFilename(filename)
			if ok {
				if err := db.Model(new(HistoryModel)).Where(where).
					Update("checksum", originalScript.Checksum()).Error; err != nil {
					return errors.Wrapf(err, "failed to patch new checksum, modeule name: %s, script name: %s, type: %s",
						mod.Name, script.GetName(), ScriptTypeSQL)
				}
			}
		default:
			return errors.New("only support .sql patch file")
		}
	}

	return nil
}

func (mig *Migrator) patchSQLAfterInstalling(s *Script, exec *gorm.DB) (err error) {
	script, ok := mig.LocalScripts.Patches.GetScriptByFilename(patchPrefix + s.GetName())
	if !ok {
		return nil
	}
	return exec.Raw(string(script.Rawtext)).Error
}

func (mig *Migrator) installSQL(s *Script, exec *gorm.DB, tx *gorm.DB, after func(tx *gorm.DB, err error)) (err error) {
	defer after(tx, err)
	defer func() {
		mig.reversing = append(mig.reversing, s.Reversing...)
	}()

	s.Reversing = nil

	for _, node := range s.DDLNodes() {
		var (
			reverse string
			ok      bool
		)
		reverse, ok, err = ddlreverser.ReverseDDLWithSnapshot(exec, node)
		if err != nil {
			return errors.Wrapf(err, "failed to generate reversed DDL: %+v",
				map[string]string{"scritpName": s.GetName(), "SQL": node.Text()})
		}
		if ok {
			s.Reversing = append(s.Reversing, reverse)
		}

		if err = exec.Exec(node.Text()).Error; err != nil {
			return errors.Wrapf(err, "failed to do schema migration: %+v",
				map[string]string{"scriptName": s.GetName(), "SQL": node.Text()})
		}
	}

	for _, node := range s.DMLNodes() {
		if err = tx.Exec(node.Text()).Error; err != nil {
			return errors.Wrapf(err, "failed to do data migration: %+v",
				map[string]string{"scriptName": s.GetName(), "SQL": node.Text()})
		}
	}

	return nil
}

func (mig *Migrator) installPy(s *Script, module *Module, settings *pygrator.Settings, commit bool) error {
	var p = pygrator.Package{
		DeveloperScript: s,
		Requirements:    module.PythonRequirementsText,
		Settings:        *settings,
		Commit:          commit,
	}
	if len(p.Requirements) == 0 {
		p.Requirements = []byte(pygrator.BaseRequirements)
	}

	if err := p.Make(); err != nil {
		return err
	}
	defer p.Remove()

	return p.Run()
}

func (mig *Migrator) destructiveLint() error {
	text, ok := mig.LocalScripts.HasDestructiveOperationInPending()
	if ok {
		return errors.Errorf("there is desctructive SQL in pending scripts, SQL: %s", text)
	}

	return nil
}

func (mig *Migrator) compareSchemas(db *gorm.DB) (string, bool) {
	logrus.Infoln("compare local schema and cloud schema for every service...")
	var (
		reasons string
		eq      = true
	)
	for modName, service := range mig.LocalScripts.Services {
		equal := service.BaselineEqualCloud(db)
		if !equal.Equal() {
			eq = false
			reasons += fmt.Sprintf("module name: %s:\n%s", modName, equal.Reason())
		}
	}
	return reasons, eq
}

func retrievePatchesFiles(ctx context.Context) ([]string, error) {
	value := ctx.Value(patchesKey)
	if value == nil {
		logrus.Infoln("retrievePatchesFiles value is nil")
		return nil, nil
	}
	var (
		files []string
		ok    bool
	)
	files, ok = value.([]string)
	if !ok {
		return nil, errors.New("failed to retrieve patches files list")
	}
	return files, nil
}
