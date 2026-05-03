package filestore

import (
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/scheduler"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
)

// NewPersonalServices creates a Services instance for personal (single-user) mode.
//
// All stores are backed by the local filesystem. The Services struct fields
// are populated incrementally as subsystems are initialized during startup
// (not all at once), since the existing startup sequence creates stores at
// different points.
//
// After creation, callers should set individual fields as subsystems come online:
//
//	svc := filestore.NewPersonalServices()
//	svc.Sessions = filestore.NewSessionStore(sessionFS)
//	svc.Credentials = filestore.NewCredentialStore(credStore)
//	// ... etc
func NewPersonalServices() *store.Services {
	return &store.Services{
		Mode:  store.ModePersonal,
		Apps:  NewAppStore(),
		Flows: NewFlowStore(),
		Audit: NewNoopAuditStore(),
	}
}

// SetSessionStore sets the session store on a Services instance.
// This is called when the session.FileStore is constructed during startup.
func SetSessionStore(svc *store.Services, fs *session.FileStore) {
	svc.Sessions = NewSessionStore(fs)
}

// SetMemoryStores sets the memory store and manager on a Services instance.
// This is called when the memory system is initialized during startup.
func SetMemoryStores(svc *store.Services, ms *memory.Store, mm *memory.Manager) {
	if ms != nil {
		svc.Memory = NewMemoryStore(ms)
	}
	if mm != nil {
		svc.MemoryMgr = NewMemoryManager(mm)
	}
}

// SetCredentialStore sets the credential store on a Services instance.
// This is called when the credential store is opened during startup.
func SetCredentialStore(svc *store.Services, cs *credentials.Store) {
	svc.Credentials = NewCredentialStore(cs)
}

// SetSchedulerStore sets the scheduler store on a Services instance.
// This is called when the scheduler is initialized during startup.
func SetSchedulerStore(svc *store.Services, ss *scheduler.Store) {
	svc.Scheduler = NewSchedulerStore(ss)
}

// SetFleetStores sets the fleet template and plan stores on a Services instance.
// This is called when fleet registries are initialized during startup.
func SetFleetStores(svc *store.Services, reg *fleet.Registry, planReg *fleet.PlanRegistry) {
	if reg != nil {
		svc.FleetTemplates = NewFleetTemplateStore(reg)
	}
	if planReg != nil {
		svc.FleetPlans = NewFleetPlanStore(planReg)
	}
}

// SetSkillStore sets the skill store on a Services instance.
func SetSkillStore(svc *store.Services, userDir string, extraDirs []string, allowlist []string) {
	svc.Skills = NewSkillStore(userDir, extraDirs, allowlist)
}
