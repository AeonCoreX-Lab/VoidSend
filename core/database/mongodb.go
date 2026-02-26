package database

import (
	"context"
	"time"

	"github.com/VoidSend/monitoring"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/bson"
)

var MongoClient *mongo.Client
var MongoDatabase *mongo.Database

type MongoDBConfig struct {
	URL        string
	Database   string
	Collection string
}

func InitMongoDB(config *MongoDBConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.URL))
	if err != nil {
		return err
	}

	// Ping database
	if err = client.Ping(ctx, nil); err != nil {
		return err
	}

	MongoClient = client
	MongoDatabase = client.Database(config.Database)

	// Create indexes
	if err = createMongoIndexes(); err != nil {
		return err
	}

	monitoring.Info("MongoDB connected successfully")
	return nil
}

func createMongoIndexes() error {
	ctx := context.Background()
	
	// Email collection indexes
	emailColl := MongoDatabase.Collection("emails")
	
	_, err := emailColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	})
	
	return err
}

// SaveEmail saves email to MongoDB
func SaveEmail(ctx context.Context, email interface{}) error {
	coll := MongoDatabase.Collection("emails")
	_, err := coll.InsertOne(ctx, email)
	return err
}

// GetUserEmails gets user's emails from MongoDB
func GetUserEmails(ctx context.Context, userID string, limit int64) ([]bson.M, error) {
	coll := MongoDatabase.Collection("emails")
	
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit)
	
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	
	return results, nil
}

// UpdateEmailStatus updates email status
func UpdateEmailStatus(ctx context.Context, messageID, status string) error {
	coll := MongoDatabase.Collection("emails")
	
	filter := bson.M{"message_id": messageID}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}
	
	_, err := coll.UpdateOne(ctx, filter, update)
	return err
}