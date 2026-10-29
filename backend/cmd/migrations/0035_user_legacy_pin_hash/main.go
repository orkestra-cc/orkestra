// Binary 0035_user_legacy_pin_hash retires the reversible `pin` field of
// operator_users and client_users. Until the PIN sign-in method existed
// the user module stored an optional 4-digit PIN
// AES-GCM-encrypted with OAUTH_TOKEN_ENCRYPTION_KEY and never read it back.
// For every live row that still carries `pin` this binary decrypts it once,
// writes an argon2id `pinHash` (the auth module's own hasher) when the value
// is 4–8 digits and the row has no `pinHash` yet, and unsets `pin` in every
// case. Idempotent: a re-run finds no `pin` field left.
//
// Logs counts only — never a PIN, a hash or a user identifier.
// MONGO_URI / MONGO_DATABASE / OAUTH_TOKEN_ENCRYPTION_KEY; --dry-run counts
// the rows that would change without writing.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"regexp"
	"time"

	authservices "github.com/orkestra/backend/internal/core/auth/services"
	userrepo "github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	os.Exit(run())
}

func run() int {
	dryRun := flag.Bool("dry-run", false, "count the rows that would change, no writes")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	uri, dbName := os.Getenv("MONGO_URI"), os.Getenv("MONGO_DATABASE")
	if uri == "" || dbName == "" {
		logger.Error("MONGO_URI and MONGO_DATABASE are required")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		// NEVER log this error: a malformed MONGO_URI comes back verbatim,
		// credentials included.
		logger.Error("connect failed", "database", dbName)
		return 1
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	hasher := authservices.NewPasswordService(logger, false)
	for _, coll := range []string{userrepo.OperatorUsersCollection, userrepo.ClientUsersCollection} {
		res, err := migrate(ctx, client.Database(dbName).Collection(coll), hasher.Hash, utils.DecryptOAuthToken, *dryRun)
		if err != nil {
			logger.Error("migrate", "collection", coll, "error", err)
			return 1
		}
		logger.Info("done", "collection", coll, "dryRun", *dryRun,
			"candidates", res.Candidates, "hashed", res.Hashed, "kept", res.KeptExisting, "dropped", res.Dropped)
	}
	return 0
}

type result struct {
	Candidates   int // rows carrying a non-empty legacy pin
	Hashed       int // pinHash written from the decrypted value
	KeptExisting int // row already had a pinHash; legacy value discarded
	Dropped      int // undecryptable or not 4–8 digits; legacy value discarded
}

var validPIN = regexp.MustCompile(`^[0-9]{4,8}$`)

// migrate converts the legacy field of one tier collection. hash and
// decrypt are injected so the test needs neither a key nor argon2 cost.
func migrate(ctx context.Context, coll *mongo.Collection, hash func(string) (string, error), decrypt func(string) (string, error), dryRun bool) (result, error) {
	var res result
	//tenantscope:allow operator migration: user rows are global per tier, not tenant-scoped
	cur, err := coll.Find(ctx, bson.M{"pin": bson.M{"$exists": true}})
	if err != nil {
		return res, err
	}
	defer func() { _ = cur.Close(ctx) }()
	for cur.Next(ctx) {
		var row struct {
			UUID    string `bson:"uuid"`
			PIN     string `bson:"pin"`
			PINHash string `bson:"pinHash"`
		}
		if err := cur.Decode(&row); err != nil {
			return res, err
		}
		set := bson.M{}
		switch {
		case row.PIN == "":
			// An empty legacy value carries nothing to convert.
		case row.PINHash != "":
			res.Candidates++
			res.KeptExisting++
		default:
			res.Candidates++
			plain, derr := decrypt(row.PIN)
			if derr != nil || !validPIN.MatchString(plain) {
				res.Dropped++
				break
			}
			h, herr := hash(plain)
			if herr != nil {
				return res, herr
			}
			now := time.Now()
			set["pinHash"], set["pinUpdatedAt"] = h, now
			res.Hashed++
		}
		if dryRun {
			continue
		}
		update := bson.M{"$unset": bson.M{"pin": ""}}
		if len(set) > 0 {
			update["$set"] = set
		}
		//tenantscope:allow operator migration: user rows are global per tier, not tenant-scoped
		if _, err := coll.UpdateOne(ctx, bson.M{"uuid": row.UUID, "pin": bson.M{"$exists": true}}, update); err != nil {
			return res, err
		}
	}
	return res, cur.Err()
}
