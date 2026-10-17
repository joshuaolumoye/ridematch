package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// allVehicleTypes is the fixed set of vehicle types the platform
// supports. Enabling/disabling one is data (VehicleTypeConfig), not a
// change to this list — a disabled type still exists, it's just hidden
// from registration and the booking picker.
var allVehicleTypes = []models.VehicleType{models.VehicleCar, models.VehicleOkada, models.VehicleKeke, models.VehicleBus}

// SettingsService implements every admin-configurable platform setting
// introduced alongside per-vehicle-type payment plans and document
// requirements: which vehicle types currently accept new drivers, which
// billing cycles each vehicle type offers (and at what price), and which
// documents each vehicle type's drivers must/may submit. It's used both
// by AdminHandler (full read/write access) and by the driver/passenger-
// facing "meta" endpoints (read-only, enabled-only views).
type SettingsService struct {
	vehicleTypes repository.VehicleTypeConfigRepository
	plans        repository.SubscriptionPlanRepository
	docTypes     repository.DocumentTypeRepository
	catalog      repository.VehicleCatalogRepository
}

// NewSettingsService constructs a SettingsService.
func NewSettingsService(
	vehicleTypes repository.VehicleTypeConfigRepository,
	plans repository.SubscriptionPlanRepository,
	docTypes repository.DocumentTypeRepository,
	catalog repository.VehicleCatalogRepository,
) *SettingsService {
	return &SettingsService{vehicleTypes: vehicleTypes, plans: plans, docTypes: docTypes, catalog: catalog}
}

// ListVehicleTypes returns all four vehicle types with their current
// enabled flag, defaulting to enabled=true for any type that has no
// config row yet (a fresh, un-migrated environment) — "no explicit
// setting" means "on", never "silently hidden".
func (s *SettingsService) ListVehicleTypes(ctx context.Context) ([]dto.AdminVehicleTypeItem, error) {
	rows, err := s.vehicleTypes.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list vehicle type configs: %w", err)
	}
	byType := make(map[models.VehicleType]models.VehicleTypeConfig, len(rows))
	for _, r := range rows {
		byType[r.VehicleType] = r
	}

	items := make([]dto.AdminVehicleTypeItem, 0, len(allVehicleTypes))
	for _, vt := range allVehicleTypes {
		if cfg, ok := byType[vt]; ok {
			items = append(items, toVehicleTypeItem(&cfg))
			continue
		}
		items = append(items, dto.AdminVehicleTypeItem{VehicleType: string(vt), Enabled: true})
	}
	return items, nil
}

// EnabledVehicleTypes returns only the vehicle types currently accepting
// new drivers — the driver-facing /meta/vehicle-types shape, and what
// the registration and booking screens should render.
func (s *SettingsService) EnabledVehicleTypes(ctx context.Context) ([]dto.VehicleTypeItem, error) {
	all, err := s.ListVehicleTypes(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]dto.VehicleTypeItem, 0, len(all))
	for _, v := range all {
		if v.Enabled {
			items = append(items, dto.VehicleTypeItem{VehicleType: v.VehicleType})
		}
	}
	return items, nil
}

// IsVehicleTypeEnabled reports whether a vehicle type currently accepts
// new drivers.
func (s *SettingsService) IsVehicleTypeEnabled(ctx context.Context, vehicleType string) (bool, error) {
	if !isValidVehicleType(vehicleType) {
		return false, ErrInvalidVehicleType
	}
	cfg, err := s.vehicleTypes.FindByVehicleType(ctx, models.VehicleType(vehicleType))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return true, nil // no row yet — default to enabled
		}
		return false, fmt.Errorf("service: failed to load vehicle type config: %w", err)
	}
	return cfg.Enabled, nil
}

// SetVehicleTypeEnabled turns one vehicle type on or off.
func (s *SettingsService) SetVehicleTypeEnabled(ctx context.Context, vehicleType string, enabled bool) (*dto.AdminVehicleTypeItem, error) {
	if !isValidVehicleType(vehicleType) {
		return nil, ErrInvalidVehicleType
	}
	cfg := &models.VehicleTypeConfig{VehicleType: models.VehicleType(vehicleType), Enabled: enabled, UpdatedAt: time.Now()}
	if err := s.vehicleTypes.Upsert(ctx, cfg); err != nil {
		return nil, fmt.Errorf("service: failed to update vehicle type config: %w", err)
	}
	item := toVehicleTypeItem(cfg)
	return &item, nil
}

func toVehicleTypeItem(cfg *models.VehicleTypeConfig) dto.AdminVehicleTypeItem {
	item := dto.AdminVehicleTypeItem{VehicleType: string(cfg.VehicleType), Enabled: cfg.Enabled}
	if !cfg.UpdatedAt.IsZero() {
		item.UpdatedAt = cfg.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return item
}

// ListSubscriptionPlans returns every (vehicle type, billing cycle)
// plan — enabled and disabled alike — optionally narrowed to one
// vehicle type, for the admin settings grid.
func (s *SettingsService) ListSubscriptionPlans(ctx context.Context, vehicleType string) ([]dto.AdminSubscriptionPlanItem, error) {
	var rows []models.SubscriptionPlan
	var err error
	if vehicleType != "" {
		if !isValidVehicleType(vehicleType) {
			return nil, ErrInvalidVehicleType
		}
		rows, err = s.plans.FindByVehicleType(ctx, models.VehicleType(vehicleType))
	} else {
		rows, err = s.plans.FindAll(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("service: failed to list subscription plans: %w", err)
	}

	items := make([]dto.AdminSubscriptionPlanItem, 0, len(rows))
	for i := range rows {
		items = append(items, toSubscriptionPlanItem(&rows[i]))
	}
	return items, nil
}

// EnabledSubscriptionPlans returns only the enabled billing cycles for
// one vehicle type, in cycle order — the driver-facing plan picker.
func (s *SettingsService) EnabledSubscriptionPlans(ctx context.Context, vehicleType models.VehicleType) ([]dto.SubscriptionPlanOption, error) {
	rows, err := s.plans.FindEnabledByVehicleType(ctx, vehicleType)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list enabled subscription plans: %w", err)
	}
	options := make([]dto.SubscriptionPlanOption, 0, len(rows))
	for _, r := range rows {
		options = append(options, dto.SubscriptionPlanOption{BillingCycle: string(r.BillingCycle), PriceKobo: r.PriceKobo})
	}
	return options, nil
}

// EnabledPlanFor returns the enabled plan for one vehicle type + billing
// cycle, or ErrSubscriptionPlanUnavailable if that cycle isn't currently
// turned on for that vehicle type.
func (s *SettingsService) EnabledPlanFor(ctx context.Context, vehicleType models.VehicleType, cycle models.BillingCycle) (*models.SubscriptionPlan, error) {
	plan, err := s.plans.FindByVehicleTypeAndCycle(ctx, vehicleType, cycle)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSubscriptionPlanUnavailable
		}
		return nil, fmt.Errorf("service: failed to load subscription plan: %w", err)
	}
	if !plan.Enabled {
		return nil, ErrSubscriptionPlanUnavailable
	}
	return plan, nil
}

// UpsertSubscriptionPlan sets one (vehicle type, billing cycle) plan's
// enabled flag and price.
func (s *SettingsService) UpsertSubscriptionPlan(ctx context.Context, vehicleType, cycle string, enabled bool, priceKobo int64) (*dto.AdminSubscriptionPlanItem, error) {
	if !isValidVehicleType(vehicleType) {
		return nil, ErrInvalidVehicleType
	}
	if !models.IsValidBillingCycle(cycle) {
		return nil, fmt.Errorf("service: invalid billing cycle %q", cycle)
	}

	plan := &models.SubscriptionPlan{
		VehicleType:  models.VehicleType(vehicleType),
		BillingCycle: models.BillingCycle(cycle),
		Enabled:      enabled,
		PriceKobo:    priceKobo,
		UpdatedAt:    time.Now(),
	}
	if err := s.plans.Upsert(ctx, plan); err != nil {
		return nil, fmt.Errorf("service: failed to update subscription plan: %w", err)
	}
	item := toSubscriptionPlanItem(plan)
	return &item, nil
}

func toSubscriptionPlanItem(p *models.SubscriptionPlan) dto.AdminSubscriptionPlanItem {
	item := dto.AdminSubscriptionPlanItem{
		VehicleType:  string(p.VehicleType),
		BillingCycle: string(p.BillingCycle),
		Enabled:      p.Enabled,
		PriceKobo:    p.PriceKobo,
	}
	if !p.UpdatedAt.IsZero() {
		item.UpdatedAt = p.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return item
}

// ListDocumentTypes returns a vehicle type's document requirements,
// enabled and disabled alike (admin view). An empty vehicleType returns
// every vehicle type's document types.
func (s *SettingsService) ListDocumentTypes(ctx context.Context, vehicleType string) ([]dto.AdminDocumentTypeItem, error) {
	var rows []models.DocumentType
	var err error
	if vehicleType != "" {
		if !isValidVehicleType(vehicleType) {
			return nil, ErrInvalidVehicleType
		}
		rows, err = s.docTypes.FindByVehicleType(ctx, models.VehicleType(vehicleType))
	} else {
		rows, err = s.docTypes.FindAll(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("service: failed to list document types: %w", err)
	}

	items := make([]dto.AdminDocumentTypeItem, 0, len(rows))
	for i := range rows {
		items = append(items, toDocumentTypeItem(&rows[i]))
	}
	return items, nil
}

// EnabledDocumentTypes returns only the enabled document types for one
// vehicle type, in SortOrder — what the registration screen renders.
func (s *SettingsService) EnabledDocumentTypes(ctx context.Context, vehicleType models.VehicleType) ([]models.DocumentType, error) {
	rows, err := s.docTypes.FindEnabledByVehicleType(ctx, vehicleType)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list enabled document types: %w", err)
	}
	return rows, nil
}

// CreateDocumentType adds a new document requirement to a vehicle type.
func (s *SettingsService) CreateDocumentType(ctx context.Context, req dto.AdminCreateDocumentTypeRequest) (*dto.AdminDocumentTypeItem, error) {
	docType := &models.DocumentType{
		VehicleType: models.VehicleType(req.VehicleType),
		Name:        req.Name,
		Required:    req.Required,
		Enabled:     true,
		SortOrder:   req.SortOrder,
	}
	if err := s.docTypes.Create(ctx, docType); err != nil {
		return nil, fmt.Errorf("service: failed to create document type: %w", err)
	}
	item := toDocumentTypeItem(docType)
	return &item, nil
}

// UpdateDocumentType edits an existing document requirement (name,
// required/optional, enabled, sort order).
func (s *SettingsService) UpdateDocumentType(ctx context.Context, id string, req dto.AdminUpdateDocumentTypeRequest) (*dto.AdminDocumentTypeItem, error) {
	docType, err := s.docTypes.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDocumentTypeNotFound
		}
		return nil, fmt.Errorf("service: failed to load document type: %w", err)
	}

	docType.Name = req.Name
	docType.Required = req.Required
	docType.Enabled = req.Enabled
	docType.SortOrder = req.SortOrder
	if err := s.docTypes.Update(ctx, docType); err != nil {
		return nil, fmt.Errorf("service: failed to update document type: %w", err)
	}
	item := toDocumentTypeItem(docType)
	return &item, nil
}

// DeleteDocumentType removes a document requirement entirely. Driver
// documents already submitted against it are left as-is (historical
// record); it simply stops being required/shown going forward.
func (s *SettingsService) DeleteDocumentType(ctx context.Context, id string) error {
	if _, err := s.docTypes.FindByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrDocumentTypeNotFound
		}
		return fmt.Errorf("service: failed to load document type: %w", err)
	}
	if err := s.docTypes.Delete(ctx, id); err != nil {
		return fmt.Errorf("service: failed to delete document type: %w", err)
	}
	return nil
}

func toDocumentTypeItem(d *models.DocumentType) dto.AdminDocumentTypeItem {
	return dto.AdminDocumentTypeItem{
		ID:          d.ID,
		VehicleType: string(d.VehicleType),
		Name:        d.Name,
		Required:    d.Required,
		Enabled:     d.Enabled,
		SortOrder:   d.SortOrder,
	}
}

// --- Vehicle make/model catalog ---
//
// There's no vehicle-data API worth integrating here: the mainstream
// ones (NHTSA's vPIC, CarQuery, etc.) are US/EU car-centric and simply
// don't carry okada/keke (motorcycle/tricycle) makes, or Nigeria-market
// trims. So this catalog is entirely admin-curated, same pattern as
// vehicle types/documents/plans above. Year of manufacture is
// deliberately not part of this catalog at all — see VehicleModel's doc
// comment.

// ListVehicleMakes returns one vehicle type's full make/model tree
// (enabled and disabled alike) — the admin catalog editor's data.
func (s *SettingsService) ListVehicleMakes(ctx context.Context, vehicleType string) ([]dto.AdminVehicleMakeItem, error) {
	if !isValidVehicleType(vehicleType) {
		return nil, ErrInvalidVehicleType
	}
	rows, err := s.catalog.FindMakesByVehicleType(ctx, models.VehicleType(vehicleType))
	if err != nil {
		return nil, fmt.Errorf("service: failed to list vehicle makes: %w", err)
	}
	items := make([]dto.AdminVehicleMakeItem, 0, len(rows))
	for i := range rows {
		items = append(items, toAdminMakeItem(&rows[i]))
	}
	return items, nil
}

// VehicleCatalog returns one vehicle type's *enabled* make/model tree —
// what the driver registration screen and the passenger booking
// screen's category icons actually render.
func (s *SettingsService) VehicleCatalog(ctx context.Context, vehicleType models.VehicleType) ([]dto.VehicleCatalogMakeItem, error) {
	rows, err := s.catalog.FindMakesByVehicleType(ctx, vehicleType)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load vehicle catalog: %w", err)
	}
	items := make([]dto.VehicleCatalogMakeItem, 0, len(rows))
	for _, make_ := range rows {
		if !make_.Enabled {
			continue
		}
		models_ := make([]dto.VehicleCatalogModelItem, 0, len(make_.Models))
		for _, m := range make_.Models {
			if !m.Enabled {
				continue
			}
			models_ = append(models_, dto.VehicleCatalogModelItem{ID: m.ID, Name: m.Name, Category: string(m.Category)})
		}
		if len(models_) == 0 {
			continue // a make with no enabled models isn't pickable — leave it off the list
		}
		items = append(items, dto.VehicleCatalogMakeItem{ID: make_.ID, Name: make_.Name, Models: models_})
	}
	return items, nil
}

// CreateVehicleMake adds a new manufacturer under a vehicle type.
func (s *SettingsService) CreateVehicleMake(ctx context.Context, req dto.AdminCreateMakeRequest) (*dto.AdminVehicleMakeItem, error) {
	if !isValidVehicleType(req.VehicleType) {
		return nil, ErrInvalidVehicleType
	}
	if _, err := s.catalog.FindMakeByName(ctx, models.VehicleType(req.VehicleType), req.Name); err == nil {
		return nil, ErrDuplicateVehicleMake
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing vehicle make: %w", err)
	}

	make_ := &models.VehicleMake{VehicleType: models.VehicleType(req.VehicleType), Name: req.Name, Enabled: true}
	if err := s.catalog.CreateMake(ctx, make_); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrDuplicateVehicleMake
		}
		return nil, fmt.Errorf("service: failed to create vehicle make: %w", err)
	}
	item := toAdminMakeItem(make_)
	return &item, nil
}

// UpdateVehicleMake edits a make's name and/or enabled flag.
func (s *SettingsService) UpdateVehicleMake(ctx context.Context, id string, req dto.AdminUpdateMakeRequest) (*dto.AdminVehicleMakeItem, error) {
	make_, err := s.catalog.FindMakeByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrVehicleMakeNotFound
		}
		return nil, fmt.Errorf("service: failed to load vehicle make: %w", err)
	}
	if existing, err := s.catalog.FindMakeByName(ctx, make_.VehicleType, req.Name); err == nil && existing.ID != id {
		return nil, ErrDuplicateVehicleMake
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing vehicle make: %w", err)
	}

	make_.Name = req.Name
	make_.Enabled = req.Enabled
	if err := s.catalog.UpdateMake(ctx, make_); err != nil {
		return nil, fmt.Errorf("service: failed to update vehicle make: %w", err)
	}
	item := toAdminMakeItem(make_)
	return &item, nil
}

// DeleteVehicleMake removes a make and every model under it. Drivers
// already registered against it keep their snapshotted make/model name
// (see DriverProfile) — this only affects what's pickable going forward.
func (s *SettingsService) DeleteVehicleMake(ctx context.Context, id string) error {
	if _, err := s.catalog.FindMakeByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrVehicleMakeNotFound
		}
		return fmt.Errorf("service: failed to load vehicle make: %w", err)
	}
	if err := s.catalog.DeleteMake(ctx, id); err != nil {
		return fmt.Errorf("service: failed to delete vehicle make: %w", err)
	}
	return nil
}

// CreateVehicleModel adds a new model/trim under an existing make.
func (s *SettingsService) CreateVehicleModel(ctx context.Context, req dto.AdminCreateModelRequest) (*dto.AdminVehicleModelItem, error) {
	make_, err := s.catalog.FindMakeByID(ctx, req.MakeID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrVehicleMakeNotFound
		}
		return nil, fmt.Errorf("service: failed to load vehicle make: %w", err)
	}
	if _, err := s.catalog.FindModelByName(ctx, req.MakeID, req.Name); err == nil {
		return nil, ErrDuplicateVehicleModel
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing vehicle model: %w", err)
	}

	category := models.VehicleCategory(req.Category)
	if make_.VehicleType != models.VehicleCar {
		category = "" // category only means anything for cars
	}
	model := &models.VehicleModel{MakeID: req.MakeID, Name: req.Name, Category: category, Enabled: true}
	if err := s.catalog.CreateModel(ctx, model); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrDuplicateVehicleModel
		}
		return nil, fmt.Errorf("service: failed to create vehicle model: %w", err)
	}
	item := toAdminModelItem(model)
	return &item, nil
}

// UpdateVehicleModel edits a model's name, category, and/or enabled flag.
func (s *SettingsService) UpdateVehicleModel(ctx context.Context, id string, req dto.AdminUpdateModelRequest) (*dto.AdminVehicleModelItem, error) {
	model, err := s.catalog.FindModelByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrVehicleModelNotFound
		}
		return nil, fmt.Errorf("service: failed to load vehicle model: %w", err)
	}
	if existing, err := s.catalog.FindModelByName(ctx, model.MakeID, req.Name); err == nil && existing.ID != id {
		return nil, ErrDuplicateVehicleModel
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check existing vehicle model: %w", err)
	}

	model.Name = req.Name
	model.Category = models.VehicleCategory(req.Category)
	model.Enabled = req.Enabled
	if err := s.catalog.UpdateModel(ctx, model); err != nil {
		return nil, fmt.Errorf("service: failed to update vehicle model: %w", err)
	}
	item := toAdminModelItem(model)
	return &item, nil
}

// DeleteVehicleModel removes a single model, leaving its make and
// sibling models untouched.
func (s *SettingsService) DeleteVehicleModel(ctx context.Context, id string) error {
	if _, err := s.catalog.FindModelByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrVehicleModelNotFound
		}
		return fmt.Errorf("service: failed to load vehicle model: %w", err)
	}
	if err := s.catalog.DeleteModel(ctx, id); err != nil {
		return fmt.Errorf("service: failed to delete vehicle model: %w", err)
	}
	return nil
}

// BulkImportVehicleCatalog creates many make+model rows in one call —
// the admin dashboard's "paste a sheet of rows" importer. Idempotent by
// design: re-pasting the same sheet (or one that overlaps an earlier
// import) just skips rows that already exist (case-insensitive
// make+model match) rather than erroring or duplicating, so admin can
// safely paste a growing master list over and over as it's extended.
func (s *SettingsService) BulkImportVehicleCatalog(ctx context.Context, req dto.AdminBulkCatalogRequest) (*dto.AdminBulkCatalogResult, error) {
	if !isValidVehicleType(req.VehicleType) {
		return nil, ErrInvalidVehicleType
	}
	vehicleType := models.VehicleType(req.VehicleType)
	result := &dto.AdminBulkCatalogResult{}

	// Cache makes already resolved/created during this import so 50 rows
	// of the same make only look it up (or create it) once.
	makeIDByName := make(map[string]string)

	for _, row := range req.Rows {
		makeName := strings.TrimSpace(row.Make)
		modelName := strings.TrimSpace(row.Model)
		if makeName == "" || modelName == "" {
			result.Skipped++
			continue
		}
		key := strings.ToLower(makeName)

		makeID, cached := makeIDByName[key]
		if !cached {
			existing, err := s.catalog.FindMakeByName(ctx, vehicleType, makeName)
			switch {
			case err == nil:
				makeID = existing.ID
			case errors.Is(err, repository.ErrNotFound):
				newMake := &models.VehicleMake{VehicleType: vehicleType, Name: makeName, Enabled: true}
				if err := s.catalog.CreateMake(ctx, newMake); err != nil {
					if errors.Is(err, repository.ErrDuplicate) {
						// Someone else created the same make in the gap
						// between our lookup and this insert — re-fetch
						// rather than fail the whole import over a race.
						existing, findErr := s.catalog.FindMakeByName(ctx, vehicleType, makeName)
						if findErr != nil {
							return nil, fmt.Errorf("service: failed to re-resolve vehicle make %q after a duplicate during bulk import: %w", makeName, findErr)
						}
						makeID = existing.ID
						break
					}
					return nil, fmt.Errorf("service: failed to create vehicle make %q during bulk import: %w", makeName, err)
				} else {
					makeID = newMake.ID
					result.MakesCreated++
				}
			default:
				return nil, fmt.Errorf("service: failed to look up vehicle make %q during bulk import: %w", makeName, err)
			}
			makeIDByName[key] = makeID
		}

		if _, err := s.catalog.FindModelByName(ctx, makeID, modelName); err == nil {
			result.Skipped++
			continue
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: failed to look up vehicle model %q during bulk import: %w", modelName, err)
		}

		category := models.VehicleCategory(row.Category)
		if vehicleType != models.VehicleCar {
			category = ""
		}
		newModel := &models.VehicleModel{MakeID: makeID, Name: modelName, Category: category, Enabled: true}
		if err := s.catalog.CreateModel(ctx, newModel); err != nil {
			if errors.Is(err, repository.ErrDuplicate) {
				// Same race as above, one level down — already exists,
				// so this row is a no-op rather than a hard failure.
				result.Skipped++
				continue
			}
			return nil, fmt.Errorf("service: failed to create vehicle model %q during bulk import: %w", modelName, err)
		}
		result.ModelsCreated++
	}

	return result, nil
}

// ResolveVehicleCatalogSelection validates a driver registration's
// chosen make+model against the live catalog — the model must exist,
// belong to makeID (when given) and to the registering vehicleType, and
// both the make and model must currently be enabled. Returns the
// make/model *names* and the model's category to snapshot onto the new
// DriverProfile (see DriverProfile's doc comment for why names are
// snapshotted rather than resolved live on every read).
func (s *SettingsService) ResolveVehicleCatalogSelection(ctx context.Context, vehicleType models.VehicleType, makeID *string, modelID string) (makeName, modelName string, category models.VehicleCategory, err error) {
	model, err := s.catalog.FindModelByID(ctx, modelID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", "", ErrVehicleModelNotFound
		}
		return "", "", "", fmt.Errorf("service: failed to load vehicle model: %w", err)
	}
	if !model.Enabled {
		return "", "", "", ErrVehicleCatalogEntryDisabled
	}
	if makeID != nil && *makeID != "" && model.MakeID != *makeID {
		return "", "", "", ErrVehicleModelMakeMismatch
	}

	make_, err := s.catalog.FindMakeByID(ctx, model.MakeID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", "", "", ErrVehicleMakeNotFound
		}
		return "", "", "", fmt.Errorf("service: failed to load vehicle make: %w", err)
	}
	if !make_.Enabled {
		return "", "", "", ErrVehicleCatalogEntryDisabled
	}
	if make_.VehicleType != vehicleType {
		return "", "", "", ErrVehicleModelMakeMismatch
	}

	return make_.Name, model.Name, model.Category, nil
}

func toAdminMakeItem(m *models.VehicleMake) dto.AdminVehicleMakeItem {
	modelItems := make([]dto.AdminVehicleModelItem, 0, len(m.Models))
	for i := range m.Models {
		modelItems = append(modelItems, toAdminModelItem(&m.Models[i]))
	}
	return dto.AdminVehicleMakeItem{
		ID:          m.ID,
		VehicleType: string(m.VehicleType),
		Name:        m.Name,
		Enabled:     m.Enabled,
		Models:      modelItems,
	}
}

func toAdminModelItem(m *models.VehicleModel) dto.AdminVehicleModelItem {
	return dto.AdminVehicleModelItem{
		ID:       m.ID,
		MakeID:   m.MakeID,
		Name:     m.Name,
		Category: string(m.Category),
		Enabled:  m.Enabled,
	}
}
