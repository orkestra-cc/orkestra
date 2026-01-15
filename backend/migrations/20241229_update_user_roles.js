/**
 * Migration Script: Update User Role Hierarchy
 *
 * This script migrates the existing user roles from the old English-based system
 * to the new Italian-based role hierarchy:
 *
 * Old -> New mapping:
 * - developer -> ceo
 * - super_admin -> amministratore
 * - administrator -> amministratore
 * - manager -> manager (unchanged)
 * - operator -> operatore
 *
 * New hierarchy (highest to lowest):
 * 1. ceo (CEO)
 * 2. amministratore (Administrator)
 * 3. manager (Manager)
 * 4. operatore (Operator)
 * 5. ospite (Guest) - new role
 */

// Migration script for MongoDB
const migrationUp = async (db) => {
  console.log('🔄 Starting user role migration...');

  try {
    // Get the users collection
    const usersCollection = db.collection('users');

    // Count existing users by role before migration
    const beforeCounts = await usersCollection.aggregate([
      { $group: { _id: '$role', count: { $sum: 1 } } },
      { $sort: { _id: 1 } }
    ]).toArray();

    console.log('📊 Current role distribution:');
    beforeCounts.forEach(item => {
      console.log(`  - ${item._id}: ${item.count} users`);
    });

    // Role mapping for migration
    const roleMapping = {
      'developer': 'ceo',
      'super_admin': 'amministratore',
      'administrator': 'amministratore',
      'manager': 'manager',  // unchanged
      'operator': 'operatore'
    };

    let totalUpdated = 0;

    // Perform role updates
    for (const [oldRole, newRole] of Object.entries(roleMapping)) {
      const result = await usersCollection.updateMany(
        { role: oldRole },
        {
          $set: {
            role: newRole,
            updatedAt: new Date()
          }
        }
      );

      if (result.modifiedCount > 0) {
        console.log(`✅ Updated ${result.modifiedCount} users from '${oldRole}' to '${newRole}'`);
        totalUpdated += result.modifiedCount;
      }
    }

    // Count users by role after migration
    const afterCounts = await usersCollection.aggregate([
      { $group: { _id: '$role', count: { $sum: 1 } } },
      { $sort: { _id: 1 } }
    ]).toArray();

    console.log('📊 Updated role distribution:');
    afterCounts.forEach(item => {
      console.log(`  - ${item._id}: ${item.count} users`);
    });

    // Validate migration - check for any unmapped roles
    const unmappedRoles = await usersCollection.distinct('role', {
      role: { $nin: ['ceo', 'amministratore', 'manager', 'operatore', 'ospite'] }
    });

    if (unmappedRoles.length > 0) {
      console.warn('⚠️  Found users with unmapped roles:', unmappedRoles);
      console.warn('   These users may need manual intervention.');
    }

    console.log(`✅ Migration completed successfully. Updated ${totalUpdated} users.`);

    return {
      success: true,
      totalUpdated,
      beforeCounts,
      afterCounts,
      unmappedRoles
    };

  } catch (error) {
    console.error('❌ Migration failed:', error);
    throw error;
  }
};

// Rollback script (reverses the migration)
const migrationDown = async (db) => {
  console.log('🔄 Starting user role rollback...');

  try {
    const usersCollection = db.collection('users');

    // Reverse role mapping for rollback
    const reverseMapping = {
      'ceo': 'developer',
      'amministratore': 'super_admin',  // Note: choosing super_admin as default
      'operatore': 'operator'
      // manager stays the same
    };

    let totalReverted = 0;

    for (const [currentRole, originalRole] of Object.entries(reverseMapping)) {
      const result = await usersCollection.updateMany(
        { role: currentRole },
        {
          $set: {
            role: originalRole,
            updatedAt: new Date()
          }
        }
      );

      if (result.modifiedCount > 0) {
        console.log(`✅ Reverted ${result.modifiedCount} users from '${currentRole}' to '${originalRole}'`);
        totalReverted += result.modifiedCount;
      }
    }

    console.log(`✅ Rollback completed. Reverted ${totalReverted} users.`);

    return {
      success: true,
      totalReverted
    };

  } catch (error) {
    console.error('❌ Rollback failed:', error);
    throw error;
  }
};

// CLI execution helper
const runMigration = async (direction = 'up') => {
  const { MongoClient } = require('mongodb');

  // Get MongoDB connection URL from environment or default
  const mongoUrl = process.env.MONGODB_URL || 'mongodb://localhost:27017';
  const dbName = process.env.MONGODB_DATABASE || 'orkestra';

  const client = new MongoClient(mongoUrl);

  try {
    await client.connect();
    console.log(`🔗 Connected to MongoDB: ${mongoUrl}/${dbName}`);

    const db = client.db(dbName);

    if (direction === 'up') {
      return await migrationUp(db);
    } else if (direction === 'down') {
      return await migrationDown(db);
    } else {
      throw new Error('Invalid direction. Use "up" or "down".');
    }

  } finally {
    await client.close();
    console.log('🔌 MongoDB connection closed');
  }
};

// Export for use in Node.js migration scripts
module.exports = {
  migrationUp,
  migrationDown,
  runMigration
};

// CLI execution
if (require.main === module) {
  const direction = process.argv[2] || 'up';

  runMigration(direction)
    .then((result) => {
      console.log('\n🎉 Migration script completed successfully!');
      console.log('Result:', JSON.stringify(result, null, 2));
      process.exit(0);
    })
    .catch((error) => {
      console.error('\n💥 Migration script failed!');
      console.error('Error:', error);
      process.exit(1);
    });
}