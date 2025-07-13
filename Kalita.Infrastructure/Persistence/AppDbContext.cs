using Microsoft.EntityFrameworkCore;
using Kalita.Domain.Entities;

namespace Kalita.Infrastructure.Persistence;

public class AppDbContext : DbContext
{
    public DbSet<Estimate> Estimates => Set<Estimate>();
    public DbSet<User> Users => Set<User>();
    public DbSet<Role> Roles => Set<Role>();
    public DbSet<WorkflowStepHistory> WorkflowStepHistories => Set<WorkflowStepHistory>();
    public DbSet<Project> Projects => Set<Project>();
    public DbSet<Expense> Expenses => Set<Expense>();
    public DbSet<Invoice> Invoices => Set<Invoice>();
    public DbSet<Counterparty> Counterparties => Set<Counterparty>();
    public DbSet<BudgetCategory> BudgetCategories => Set<BudgetCategory>();

    public AppDbContext(DbContextOptions<AppDbContext> options) : base(options) { }
}