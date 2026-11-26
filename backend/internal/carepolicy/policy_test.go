package carepolicy

import "testing"

func TestRoleCapabilities(t *testing.T) {
	cases := []struct {
		duty   Duty
		action Action
		want   bool
	}{
		{Supervisor, ManageStaff, true},
		{Supervisor, ReadHealthData, true},
		{Supervisor, ReadLocations, true},
		{Supervisor, IssueCommands, true},
		{Supervisor, ReadCommands, true},
		{Supervisor, ConfigureCommands, true},
		{Supervisor, ReviewAlerts, true},
		{Supervisor, ReadReviews, true},
		{Supervisor, ManageRules, true},
		{Supervisor, ReadRules, true},
		{Supervisor, ReadOperations, true},
		{Supervisor, ReadMessages, true},
		{Supervisor, ReadContacts, true},
		{Supervisor, ReadDevices, true},
		{Supervisor, ManageDevices, true},
		{Supervisor, ReadAlerts, true},
		{Supervisor, HandleAlerts, true},
		{Supervisor, ReadThresholds, true},
		{Supervisor, ManageThresholds, true},
		{Supervisor, ManageGeofences, true},
		{Supervisor, ReadGeofences, true},
		{Supervisor, Verify, true},
		{Caregiver, ReadTasks, true},
		{Caregiver, ReadHealthData, false},
		{Caregiver, ReadLocations, false},
		{Caregiver, Acknowledge, true},
		{Caregiver, Resolve, true},
		{Caregiver, Verify, false},
		{Caregiver, ReadAudit, false},
		{Caregiver, IssueCommands, false},
		{Caregiver, ReadOperations, false},
		{Caregiver, ReadMessages, false},
		{Caregiver, ReadContacts, false},
		{Caregiver, ReadDevices, false},
		{Caregiver, HandleAlerts, false},
		{Caregiver, ManageThresholds, false},
		{Caregiver, ReadReviews, false},
		{Auditor, ReadTasks, true},
		{Auditor, ReadHealthData, false},
		{Auditor, ReadLocations, false},
		{Auditor, ReadAudit, true},
		{Auditor, Resolve, false},
		{Auditor, ManageStaff, false},
		{Auditor, ReadCommands, false},
		{Auditor, ReviewAlerts, false},
		{Auditor, ReadRules, false},
		{Auditor, ReadGeofences, false},
		{"", ReadTasks, false},
		{"unassigned", ReadAudit, false},
		{Supervisor, "unknown", false},
	}
	for _, tc := range cases {
		if got := Allowed(tc.duty, tc.action); got != tc.want {
			t.Errorf("Allowed(%q,%q) = %v, want %v", tc.duty, tc.action, got, tc.want)
		}
	}
}

func TestCaregiverMayOnlyContinueOwnTask(t *testing.T) {
	own := "42"
	other := "43"
	tests := []struct {
		duty   Duty
		action Action
		owner  *string
		want   bool
	}{
		{Caregiver, Acknowledge, nil, true},
		{Caregiver, Start, &own, true},
		{Caregiver, Resolve, &own, true},
		{Caregiver, Start, &other, false},
		{Caregiver, Resolve, &other, false},
		{Caregiver, Start, nil, false},
		{Supervisor, Start, &other, true},
		{Supervisor, Resolve, nil, true},
		{Auditor, Resolve, &own, false},
	}
	for _, tc := range tests {
		if got := AllowedForIncident(tc.duty, tc.action, 42, tc.owner); got != tc.want {
			t.Errorf("role=%q action=%q owner=%v got=%v want=%v", tc.duty, tc.action, tc.owner, got, tc.want)
		}
	}
}
