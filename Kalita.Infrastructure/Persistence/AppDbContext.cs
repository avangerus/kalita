using Microsoft.EntityFrameworkCore;
using Kalita.Domain.Entities;

namespace Kalita.Infrastructure.Persistence;

public class AppDbContext : DbContext
{

    public DbSet<User> Users => Set<User>();
    public DbSet<Role> Roles => Set<Role>();
    public DbSet<WorkflowStepHistory> WorkflowStepHistories => Set<WorkflowStepHistory>();

    public DbSet<DictionaryType> DictionaryTypes { get; set; }
    public DbSet<DictionaryItem> DictionaryItems { get; set; }
    public DbSet<EntityType> EntityTypes { get; set; }
    public DbSet<EntityField> EntityFields { get; set; }
    public DbSet<EntityItem> EntityItems { get; set; }
    



    public AppDbContext(DbContextOptions<AppDbContext> options) : base(options) { }
}