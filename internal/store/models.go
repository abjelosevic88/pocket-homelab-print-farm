package store

import (
	"encoding/json"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Conn is one person's connection to a sibling Pocket app.
type Conn struct {
	Mode      string `json:"mode,omitempty"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Costs.
	Currency string  `json:"currency"`  // EUR, BAM, USD…
	KWhPrice float64 `json:"kwh_price"` // electricity, per kWh in the currency (0.25)
	Markup   int     `json:"markup"`    // percent on top of the cost for a suggested price

	// Filament.
	LowGrams        int    `json:"low_grams"`   // a spool is running low under this
	AutoDeduct      bool   `json:"auto_deduct"` // take a finished print's grams off the loaded spool
	DefaultMaterial string `json:"default_material"`

	// The farm.
	NeedClear   bool   `json:"need_clear"`   // after a print, wait for "the bed is clear" before the next
	AutoStart   bool   `json:"auto_start"`   // start the next job on a free, clear, connected printer
	SendGcode   bool   `json:"send_gcode"`   // send the G-code to a connected printer when a job starts
	DefaultFail string `json:"default_fail"` // what a print stopped on the printer counts as (failed | cancelled)

	// The other apps.
	FilesFolder       string `json:"files_folder"`       // where model files and photos go in Pocket Files
	CalendarLong      int    `json:"calendar_long"`      // prints longer than this many hours go in Pocket Calendar (0 = none)
	TasksUpkeep       bool   `json:"tasks_upkeep"`       // upkeep that is due becomes a task in Pocket Tasks
	TasksList         string `json:"tasks_list"`         // which list (empty = the inbox)
	InventoryLocation string `json:"inventory_location"` // where finished prints go in Pocket Inventory
	MoneyCategory     string `json:"money_category"`     // the category filament bought is spent from
	MoneyAccount      string `json:"money_account"`
	MoneyOnSpool      bool   `json:"money_on_spool"` // a new spool with a price is an expense in Pocket Money
	ChatTo            string `json:"chat_to"`        // the Pocket Chat conversation finished prints are shared to
	AIHelp            bool   `json:"ai_help"`

	// Notifications.
	NotifyDone    bool   `json:"notify_done"`
	NotifyFailed  bool   `json:"notify_failed"`
	NotifyLow     bool   `json:"notify_low"`
	NotifyUpkeep  bool   `json:"notify_upkeep"`
	NotifyOffline bool   `json:"notify_offline"` // a printer stops answering or shows an error
	NotifyHour    int    `json:"notify_hour"`    // daily reminders (upkeep) go at this hour
	NotifyURL     string `json:"notify_url"`
	NotifyFormat  string `json:"notify_format"` // apprise | ntfy | json

	// Each sibling Pocket app, by slug.
	Conns map[string]Conn `json:"conns"`

	// AI: Pocket AI through Core, or an OpenAI-compatible API with the
	// person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// ---- the farm ----

// Printer is one printer.
type Printer struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	Name        string  `json:"name"`
	Model       string  `json:"model"`
	Kind        string  `json:"kind"`
	Connection  string  `json:"connection"`
	URL         string  `json:"url"`
	APIKey      string  `json:"api_key,omitempty"`
	WebcamURL   string  `json:"webcam_url"`
	BedX        float64 `json:"bed_x"`
	BedY        float64 `json:"bed_y"`
	BedZ        float64 `json:"bed_z"`
	Nozzle      float64 `json:"nozzle"`
	Materials   string  `json:"materials"`
	Watts       int     `json:"watts"`
	WearPerHour int     `json:"wear_per_hour"`
	BoughtOn    string  `json:"bought_on"`
	Price       int     `json:"price"`
	StartHours  float64 `json:"start_hours"`
	Location    string  `json:"location"`
	Color       string  `json:"color"`
	Notes       string  `json:"notes"`
	SpoolID     *int64  `json:"spool_id"`
	Photo       string  `json:"-"`
	Archived    bool    `json:"archived"`
	Position    int     `json:"position"`
	LastState   string  `json:"last_state"`
	LastSeenAt  *string `json:"last_seen_at"`
	HomeRef     string  `json:"home_ref"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`

	// worked out
	Hours    float64 `json:"hours"`  // print hours, StartHours included
	Prints   int     `json:"prints"` // finished or failed
	Failed   int     `json:"failed"`
	HasPhoto bool    `json:"has_photo"`
	HasKey   bool    `json:"has_key"`
}

// Filament is a kind of filament (brand, material, colour).
type Filament struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	Brand       string  `json:"brand"`
	Material    string  `json:"material"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Diameter    float64 `json:"diameter"`
	Density     float64 `json:"density"`
	NozzleTemp  int     `json:"nozzle_temp"`
	BedTemp     int     `json:"bed_temp"`
	NetWeight   int     `json:"net_weight"`
	SpoolWeight int     `json:"spool_weight"`
	Price       int     `json:"price"`
	URL         string  `json:"url"`
	Notes       string  `json:"notes"`
	ExternalID  string  `json:"external_id"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`

	Spools    int     `json:"spools"`     // on the shelf
	LeftGrams float64 `json:"left_grams"` // on all of them
}

// Spool is one spool.
type Spool struct {
	ID           int64   `json:"id"`
	UserID       int64   `json:"-"`
	FilamentID   int64   `json:"filament_id"`
	InitialG     float64 `json:"initial_g"`
	UsedG        float64 `json:"used_g"`
	Price        int     `json:"price"`
	BoughtOn     string  `json:"bought_on"`
	OpenedOn     string  `json:"opened_on"`
	DriedOn      string  `json:"dried_on"`
	Location     string  `json:"location"`
	Lot          string  `json:"lot"`
	Notes        string  `json:"notes"`
	Archived     bool    `json:"archived"`
	MoneyRef     string  `json:"money_ref"`
	InventoryRef string  `json:"inventory_ref"`
	ExternalID   string  `json:"external_id"`
	Source       string  `json:"source"`
	LastUsedAt   *string `json:"last_used_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    *string `json:"updated_at"`

	// from the filament
	Brand         string  `json:"brand"`
	Material      string  `json:"material"`
	Name          string  `json:"name"`
	Color         string  `json:"color"`
	Density       float64 `json:"density"`
	Diameter      float64 `json:"diameter"`
	SpoolWeight   int     `json:"spool_weight"`
	FilamentPrice int     `json:"filament_price"`
	// worked out
	LeftG     float64 `json:"left_g"`
	PrinterID *int64  `json:"printer_id"` // loaded on
	Printer   string  `json:"printer"`
}

// Label is how a spool is called in lists: "Prusament PLA Galaxy Black".
func (s Spool) Label() string {
	out := s.Material
	if s.Brand != "" {
		out = s.Brand + " " + out
	}
	if s.Name != "" {
		out += " " + s.Name
	}
	return out
}

// SpoolLog is one change to a spool's weight.
type SpoolLog struct {
	ID      int64   `json:"id"`
	SpoolID int64   `json:"spool_id"`
	PrintID *int64  `json:"print_id"`
	Kind    string  `json:"kind"`
	Grams   float64 `json:"grams"`
	Note    string  `json:"note"`
	At      string  `json:"at"`
	Title   string  `json:"title"` // the print's
}

// Model is a thing to print.
type Model struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Tags        string  `json:"tags"`
	Designer    string  `json:"designer"`
	License     string  `json:"license"`
	URL         string  `json:"url"`
	Cover       string  `json:"-"`
	Favourite   bool    `json:"favourite"`
	Archived    bool    `json:"archived"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`

	HasCover   bool    `json:"has_cover"`
	Files      int     `json:"files"`
	Kinds      string  `json:"kinds"` // "stl,gcode"
	Prints     int     `json:"prints"`
	Good       int     `json:"good"`
	LastPrint  *string `json:"last_print"`
	EstSeconds int     `json:"est_seconds"` // the first G-code's
	EstGrams   float64 `json:"est_grams"`
	SizeX      float64 `json:"size_x"`
	SizeY      float64 `json:"size_y"`
	SizeZ      float64 `json:"size_z"`
}

// ModelFile is one file of a model.
type ModelFile struct {
	ID           int64           `json:"id"`
	UserID       int64           `json:"-"`
	ModelID      int64           `json:"model_id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Path         string          `json:"-"`
	Size         int64           `json:"size"`
	SHA256       string          `json:"sha256"`
	Thumb        string          `json:"-"`
	SizeX        float64         `json:"size_x"`
	SizeY        float64         `json:"size_y"`
	SizeZ        float64         `json:"size_z"`
	Volume       float64         `json:"volume"`
	Triangles    int             `json:"triangles"`
	EstSeconds   int             `json:"est_seconds"`
	EstGrams     float64         `json:"est_grams"`
	EstMM        float64         `json:"est_mm"`
	LayerHeight  float64         `json:"layer_height"`
	Nozzle       float64         `json:"nozzle"`
	Material     string          `json:"material"`
	Slicer       string          `json:"slicer"`
	PrinterModel string          `json:"printer_model"`
	Settings     json.RawMessage `json:"settings"`
	Source       string          `json:"source"`
	SourceRef    string          `json:"source_ref"`
	CreatedAt    string          `json:"created_at"`

	HasThumb  bool `json:"has_thumb"`
	Printable bool `json:"printable"` // G-code a printer can take
}

// Job is one thing in the queue.
type Job struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"-"`
	Title      string  `json:"title"`
	ModelID    *int64  `json:"model_id"`
	FileID     *int64  `json:"file_id"`
	PrinterID  *int64  `json:"printer_id"`
	FilamentID *int64  `json:"filament_id"`
	Material   string  `json:"material"`
	Quantity   int     `json:"quantity"`
	DoneCount  int     `json:"done_count"`
	Status     string  `json:"status"`
	Position   int     `json:"position"`
	DueOn      string  `json:"due_on"`
	ForWhom    string  `json:"for_whom"`
	ContactRef string  `json:"contact_ref"`
	Price      int     `json:"price"`
	Notes      string  `json:"notes"`
	InvoiceRef string  `json:"invoice_ref"`
	TaskRef    string  `json:"task_ref"`
	Source     string  `json:"source"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  *string `json:"updated_at"`
	FinishedAt *string `json:"finished_at"`

	ModelName  string  `json:"model_name"`
	FileName   string  `json:"file_name"`
	Printer    string  `json:"printer"`
	Filament   string  `json:"filament"`
	Color      string  `json:"color"`
	EstSeconds int     `json:"est_seconds"`
	EstGrams   float64 `json:"est_grams"`
	HasCover   bool    `json:"has_cover"`
	Printing   int     `json:"printing"` // prints on now
	Failed     int     `json:"failed"`
}

// Print is one print on a printer.
type Print struct {
	ID           int64           `json:"id"`
	UserID       int64           `json:"-"`
	PrinterID    *int64          `json:"printer_id"`
	JobID        *int64          `json:"job_id"`
	ModelID      *int64          `json:"model_id"`
	FileID       *int64          `json:"file_id"`
	SpoolID      *int64          `json:"spool_id"`
	Title        string          `json:"title"`
	FileName     string          `json:"file_name"`
	Status       string          `json:"status"`
	StartedAt    string          `json:"started_at"`
	EndedAt      *string         `json:"ended_at"`
	Seconds      int             `json:"seconds"`
	EstSeconds   int             `json:"est_seconds"`
	Progress     float64         `json:"progress"`
	Grams        float64         `json:"grams"`
	EstGrams     float64         `json:"est_grams"`
	Copies       int             `json:"copies"`
	FailReason   string          `json:"fail_reason"`
	FailNote     string          `json:"fail_note"`
	Rating       int             `json:"rating"`
	Notes        string          `json:"notes"`
	Settings     json.RawMessage `json:"settings"`
	Keep         bool            `json:"keep"`
	Photo        string          `json:"-"`
	FilamentCost int             `json:"filament_cost"`
	PowerCost    int             `json:"power_cost"`
	WearCost     int             `json:"wear_cost"`
	ExternalID   string          `json:"external_id"`
	InventoryRef string          `json:"inventory_ref"`
	CalendarRef  string          `json:"calendar_ref"`
	Source       string          `json:"source"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    *string         `json:"updated_at"`

	Printer  string `json:"printer"`
	Spool    string `json:"spool"`
	Color    string `json:"color"`
	Material string `json:"material"`
	HasPhoto bool   `json:"has_photo"`
	HasCover bool   `json:"has_cover"` // the model's
	Cost     int    `json:"cost"`      // all three
}

// Upkeep is something a printer needs every so often.
type Upkeep struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"-"`
	PrinterID  int64   `json:"printer_id"`
	Title      string  `json:"title"`
	EveryHours float64 `json:"every_hours"`
	EveryDays  int     `json:"every_days"`
	LastDoneOn string  `json:"last_done_on"`
	LastHours  float64 `json:"last_hours"`
	Notes      string  `json:"notes"`
	TaskRef    string  `json:"task_ref"`
	ToldAt     string  `json:"told_at"`
	CreatedAt  string  `json:"created_at"`

	// worked out against the printer's hours and today
	Printer  string   `json:"printer"`
	DueHours *float64 `json:"due_hours"` // print hours left (negative = overdue)
	DueOn    string   `json:"due_on"`    // the day it is due by days
	Due      bool     `json:"due"`
	Soon     bool     `json:"soon"`
}

// UpkeepLog is upkeep or a repair done.
type UpkeepLog struct {
	ID        int64   `json:"id"`
	PrinterID int64   `json:"printer_id"`
	UpkeepID  *int64  `json:"upkeep_id"`
	Title     string  `json:"title"`
	DoneOn    string  `json:"done_on"`
	Hours     float64 `json:"hours"`
	Cost      int     `json:"cost"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"created_at"`
	Printer   string  `json:"printer"`
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
