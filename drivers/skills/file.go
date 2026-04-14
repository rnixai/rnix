package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/vfs"
)

// skillRequest is the JSON structure expected by Write.
type skillRequest struct {
	Action       string `json:"action"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	AllowedTools string `json:"allowed_tools"`
	Body         string `json:"body"`
}

// SkillManageFile implements vfs.VFSFile for /dev/skills/manage.
type SkillManageFile struct {
	driver        *SkillManageDriver
	result        string   // cached last operation result
	callerDevices []string // injected from process context
}

// Write processes a skill manage request (create/patch/delete).
func (f *SkillManageFile) Write(_ context.Context, data []byte) error {
	var req skillRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid skill manage request: %w", err)
	}

	callerDevices := f.callerDevices

	var err error
	switch req.Action {
	case "create":
		if req.Body == "" {
			return fmt.Errorf("body is required for create action")
		}
		err = f.driver.manager.Create(req.Name, req.Description, req.AllowedTools, req.Body, callerDevices)
	case "patch":
		if req.Body == "" {
			return fmt.Errorf("body is required for patch action")
		}
		err = f.driver.manager.Patch(req.Name, req.Body, callerDevices)
	case "delete":
		err = f.driver.manager.Delete(req.Name)
	default:
		return fmt.Errorf("unknown action %q: must be create, patch, or delete", req.Action)
	}

	if err != nil {
		f.result = fmt.Sprintf("error: %s", err)
		return err
	}

	f.result = fmt.Sprintf("success: skill %q %sd", req.Name, req.Action)
	return nil
}

// Read returns the result from the last Write operation.
func (f *SkillManageFile) Read(length int) ([]byte, error) {
	return []byte(f.result), nil
}

// Close is a no-op.
func (f *SkillManageFile) Close() error { return nil }

// Stat returns file metadata for the skill manage device.
func (f *SkillManageFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{
		Name:       "skill_manage",
		IsDevice:   true,
		DevicePath: "/dev/skills/manage",
	}, nil
}
