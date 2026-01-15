package repository

import (
	"context"
	"time"

	"github.com/orkestra/backend/internal/reporting/models"
	userModels "github.com/orkestra/backend/internal/user/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// DeadlineRepository gestisce l'accesso ai dati per le scadenze
type DeadlineRepository interface {
	GetUserDeadlines(ctx context.Context) ([]models.DeadlineItem, error)
}

type deadlineRepository struct {
	userCollection *mongo.Collection
}

// NewDeadlineRepository crea una nuova istanza di DeadlineRepository
func NewDeadlineRepository(db *mongo.Database) DeadlineRepository {
	return &deadlineRepository{
		userCollection: db.Collection("users"),
	}
}

// GetUserDeadlines recupera tutte le scadenze degli utenti (certificazioni e visite mediche)
func (r *deadlineRepository) GetUserDeadlines(ctx context.Context) ([]models.DeadlineItem, error) {
	filter := bson.M{
		"isActive":  true,
		"deletedAt": nil,
		"$or": []bson.M{
			{"licenseExpiry": bson.M{"$ne": nil}},
			{"driverCardExpiry": bson.M{"$ne": nil}},
			{"cqcExpiry": bson.M{"$ne": nil}},
			{"adrExpiry": bson.M{"$ne": nil}},
			{"medicalChecks": bson.M{"$exists": true, "$ne": []interface{}{}}},
		},
	}

	cursor, err := r.userCollection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []userModels.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	var deadlines []models.DeadlineItem

	for _, user := range users {
		// Patente
		if user.LicenseExpiry != nil {
			deadlines = append(deadlines, createDeadlineItem(
				user.UUID+"_license",
				models.EntityTypeUser,
				user.UUID,
				user.FullName,
				models.DeadlineTypeLicense,
				*user.LicenseExpiry,
				"",
				"",
				"",
			))
		}

		// Carta conducente
		if user.DriverCardExpiry != nil {
			deadlines = append(deadlines, createDeadlineItem(
				user.UUID+"_drivercard",
				models.EntityTypeUser,
				user.UUID,
				user.FullName,
				models.DeadlineTypeDriverCard,
				*user.DriverCardExpiry,
				"",
				"",
				"",
			))
		}

		// CQC
		if user.CQCExpiry != nil {
			deadlines = append(deadlines, createDeadlineItem(
				user.UUID+"_cqc",
				models.EntityTypeUser,
				user.UUID,
				user.FullName,
				models.DeadlineTypeCQC,
				*user.CQCExpiry,
				"",
				"",
				"",
			))
		}

		// ADR
		if user.ADRExpiry != nil {
			deadlines = append(deadlines, createDeadlineItem(
				user.UUID+"_adr",
				models.EntityTypeUser,
				user.UUID,
				user.FullName,
				models.DeadlineTypeADR,
				*user.ADRExpiry,
				"",
				"",
				"",
			))
		}

		// Visite mediche
		for _, medicalCheck := range user.MedicalChecks {
			if medicalCheck.Expiry != nil {
				deadlines = append(deadlines, createDeadlineItem(
					user.UUID+"_medical_"+medicalCheck.ID,
					models.EntityTypeMedical,
					user.UUID,
					user.FullName+" - "+medicalCheck.Type,
					models.DeadlineTypeMedicalCheck,
					*medicalCheck.Expiry,
					medicalCheck.Notes,
					medicalCheck.Doctor,
					medicalCheck.Where,
				))
			}
		}
	}

	return deadlines, nil
}

// createDeadlineItem helper per creare un DeadlineItem
func createDeadlineItem(
	id string,
	entityType models.EntityType,
	entityID string,
	entityName string,
	deadlineType models.DeadlineType,
	expiryDate time.Time,
	notes string,
	doctor string,
	where string,
) models.DeadlineItem {
	daysUntil := models.CalculateDaysUntilExpiry(expiryDate)
	status := models.CalculateDeadlineStatus(expiryDate)

	return models.DeadlineItem{
		ID:              id,
		EntityType:      entityType,
		EntityID:        entityID,
		EntityName:      entityName,
		DeadlineType:    deadlineType,
		ExpiryDate:      expiryDate,
		DaysUntilExpiry: daysUntil,
		Status:          status,
		Notes:           notes,
		Doctor:          doctor,
		Where:           where,
	}
}
