# Database Migrations

This directory contains database migration scripts for the Orkestra system.

## Role Migration - 20241229_update_user_roles.js

### Overview
This migration updates the user role hierarchy from English-based roles to Italian-based roles.

### Role Mapping

| Old Role | New Role | Description |
|----------|----------|-------------|
| `developer` | `ceo` | Highest privilege level |
| `super_admin` | `amministratore` | System administrator |
| `administrator` | `amministratore` | System administrator |
| `manager` | `manager` | Team manager (unchanged) |
| `operator` | `operatore` | Field operator |
| *(new)* | `ospite` | Guest user (lowest privilege) |

### New Role Hierarchy
1. **CEO** (`ceo`) - Highest privilege, access to all system features
2. **Amministratore** (`amministratore`) - System administration, user management
3. **Manager** (`manager`) - Team and task management
4. **Operatore** (`operatore`) - Field operations and task execution
5. **Ospite** (`ospite`) - Limited read-only access

### Running the Migration

#### Prerequisites
- MongoDB instance running
- Node.js environment with MongoDB driver
- Environment variables configured:
  - `MONGODB_URL` (default: `mongodb://localhost:27017`)
  - `MONGODB_DATABASE` (default: `orkestra`)

#### Execute Migration
```bash
# Run the migration (forward)
cd backend/migrations
node 20241229_update_user_roles.js up

# Rollback the migration (if needed)
node 20241229_update_user_roles.js down
```

#### Using Docker
```bash
# From the project root
docker compose exec backend node /app/migrations/20241229_update_user_roles.js up
```

### Migration Process
1. **Backup Current State** - The script logs current role distribution
2. **Role Mapping** - Updates each user's role according to the mapping table
3. **Validation** - Checks for any unmapped roles after migration
4. **Logging** - Provides detailed output of changes made

### Validation
After running the migration:
1. Check the console output for any warnings about unmapped roles
2. Verify role counts match expectations
3. Test authentication and authorization with the new roles
4. Ensure frontend displays new role labels correctly

### Rollback
If issues occur, the migration can be rolled back:
- `ceo` → `developer`
- `amministratore` → `super_admin`
- `operatore` → `operator`
- `manager` remains unchanged

**Note**: The rollback maps `amministratore` to `super_admin` by default. If you had users with both `super_admin` and `administrator` roles originally, manual intervention may be required.

### Safety Considerations
- **Test First**: Run this migration on a development/staging environment first
- **Backup Database**: Create a full database backup before running in production
- **Monitor Logs**: Check application logs after migration for any role-related errors
- **User Communication**: Inform users about role name changes if they are visible in the UI

### Troubleshooting

#### Common Issues
1. **MongoDB Connection Failed**
   - Verify MONGODB_URL is correct
   - Ensure MongoDB service is running
   - Check network connectivity

2. **Users with Unexpected Roles**
   - Check the unmapped roles list in the migration output
   - Manually update these users or add new mappings to the script

3. **Authentication Issues After Migration**
   - Verify JWT token validation uses new role names
   - Check role-based access control middleware
   - Clear user sessions if necessary

#### Verification Queries
```javascript
// Check role distribution
db.users.aggregate([
  { $group: { _id: "$role", count: { $sum: 1 } } },
  { $sort: { _id: 1 } }
])

// Find users with old role format
db.users.find({
  role: { $in: ["developer", "super_admin", "administrator", "operator"] }
})

// Verify new role hierarchy is in use
db.users.find({
  role: { $in: ["ceo", "amministratore", "manager", "operatore", "ospite"] }
}).count()
```