using Kalita.Infrastructure.Persistence;
using Kalita.Application.Services;
using Microsoft.EntityFrameworkCore;

var builder = WebApplication.CreateBuilder(args);

builder.Services.AddDbContext<AppDbContext>(
    options => options.UseInMemoryDatabase("kalita_db")); // Для старта можно InMemory

// Подключаем WorkflowEngine, подсовываем ему путь к json-конфигу
builder.Services.AddSingleton(
    new Kalita.Application.Workflow.WorkflowEngine("../Kalita.Application/Workflow/Configs/estimate.workflow.json"));

// Подключаем EstimateService (в нем будет использоваться WorkflowEngine)
builder.Services.AddScoped<Kalita.Application.Services.EstimateService>();
// Подключаем WorkflowEngine для Expense
builder.Services.AddSingleton(
    new Kalita.Application.Workflow.WorkflowEngine("../Kalita.Application/Workflow/Configs/expense.workflow.json"));
builder.Services.AddScoped<Kalita.Application.Services.ExpenseService>();


builder.Services.AddControllers();
var app = builder.Build();

app.MapControllers();
app.Run();
