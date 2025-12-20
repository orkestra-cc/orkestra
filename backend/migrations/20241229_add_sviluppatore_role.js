// Migration: Add 'sviluppatore' role support
// Description: Adds the new 'sviluppatore' role as the highest privilege role in the system
// Date: 2024-12-29

db = db.getSiblingDB('orkestra');

// Migration configuration
const migration = {
  name: "add_sviluppatore_role",
  description: "Add 'sviluppatore' role as highest privilege role",
  timestamp: new Date(),
  version: "1.0.0"
};

// Log migration start
print(`[MIGRATION START] ${migration.name} - ${migration.description}`);
print(`Timestamp: ${migration.timestamp}`);

try {
  // Step 1: Check current role distribution before migration
  print("\n=== PRE-MIGRATION ANALYSIS ===");
  const roleStats = db.users.aggregate([
    { $group: { _id: "$role", count: { $sum: 1 } } },
    { $sort: { _id: 1 } }
  ]).toArray();

  print("Current role distribution:");
  roleStats.forEach(stat => {
    print(`  ${stat._id}: ${stat.count} users`);
  });

  // Step 2: Validate that 'sviluppatore' role doesn't already exist
  const existingSviluppatori = db.users.countDocuments({ role: "sviluppatore" });
  print(`\nExisting sviluppatore users: ${existingSviluppatori}`);

  // Step 3: Optional - Convert specific CEO users to sviluppatore if needed
  // Uncomment and modify the query below if you want to promote specific users

  /*
  print("\n=== PROMOTING SPECIFIC USERS TO SVILUPPATORE ===");

  // Example: Promote specific users by email
  const usersToPromote = [
    // "developer@company.com",
    // "tech-lead@company.com"
  ];

  if (usersToPromote.length > 0) {
    const promotionResult = db.users.updateMany(
      {
        email: { $in: usersToPromote },
        role: "ceo"
      },
      {
        $set: {
          role: "sviluppatore",
          updatedAt: new Date()
        }
      }
    );

    print(`Promoted ${promotionResult.modifiedCount} users to sviluppatore role`);

    // Log which users were promoted
    const promotedUsers = db.users.find(
      { email: { $in: usersToPromote }, role: "sviluppatore" },
      { email: 1, fullName: 1, role: 1 }
    ).toArray();

    print("Promoted users:");
    promotedUsers.forEach(user => {
      print(`  - ${user.fullName} (${user.email})`);
    });
  } else {
    print("No users specified for promotion to sviluppatore");
  }
  */

  // Step 4: Create validation that ensures role constraints are met
  print("\n=== VALIDATION ===");

  // Check for any invalid roles
  const validRoles = ["sviluppatore", "ceo", "amministratore", "manager", "operatore", "ospite"];
  const invalidRoles = db.users.find(
    { role: { $nin: validRoles } },
    { email: 1, role: 1 }
  ).toArray();

  if (invalidRoles.length > 0) {
    print("WARNING: Found users with invalid roles:");
    invalidRoles.forEach(user => {
      print(`  - ${user.email}: ${user.role}`);
    });
  } else {
    print("✓ All user roles are valid");
  }

  // Step 5: Post-migration role distribution
  print("\n=== POST-MIGRATION ANALYSIS ===");
  const newRoleStats = db.users.aggregate([
    { $group: { _id: "$role", count: { $sum: 1 } } },
    { $sort: { _id: 1 } }
  ]).toArray();

  print("Updated role distribution:");
  newRoleStats.forEach(stat => {
    print(`  ${stat._id}: ${stat.count} users`);
  });

  // Step 6: Create indexes if they don't exist
  print("\n=== INDEX OPTIMIZATION ===");

  // Ensure role field is indexed for performance
  try {
    db.users.createIndex({ role: 1 }, { background: true });
    print("✓ Created index on role field");
  } catch (e) {
    if (e.code === 85) { // Index already exists
      print("✓ Index on role field already exists");
    } else {
      throw e;
    }
  }

  // Step 7: Record migration in history
  const migrationRecord = {
    ...migration,
    status: "completed",
    completedAt: new Date(),
    changes: {
      roleHierarchy: {
        old: ["ceo", "amministratore", "manager", "operatore", "ospite"],
        new: ["sviluppatore", "ceo", "amministratore", "manager", "operatore", "ospite"]
      },
      usersModified: 0, // Update this if you promoted users above
      newRoleAdded: "sviluppatore"
    }
  };

  // Create migration history collection if it doesn't exist
  if (!db.getCollectionNames().includes('migration_history')) {
    db.createCollection('migration_history');
    print("✓ Created migration_history collection");
  }

  // Record this migration
  db.migration_history.insertOne(migrationRecord);
  print("✓ Migration recorded in history");

  print(`\n[MIGRATION COMPLETED] ${migration.name}`);
  print("The 'sviluppatore' role has been successfully added to the system.");
  print("Role hierarchy is now: sviluppatore > ceo > amministratore > manager > operatore > ospite");

} catch (error) {
  print(`\n[MIGRATION FAILED] ${migration.name}`);
  print(`Error: ${error.message}`);

  // Record failed migration
  const failedMigrationRecord = {
    ...migration,
    status: "failed",
    failedAt: new Date(),
    error: error.message
  };

  try {
    db.migration_history.insertOne(failedMigrationRecord);
  } catch (recordError) {
    print(`Failed to record migration failure: ${recordError.message}`);
  }

  throw error;
}