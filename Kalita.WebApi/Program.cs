using Kalita.Infrastructure.Persistence;
using Kalita.Application.Services;
using Microsoft.EntityFrameworkCore;
using Kalita.Application.Workflow;
using Microsoft.OpenApi.Models;
using Kalita.Application.Services;


var builder = WebApplication.CreateBuilder(args);

builder.Services.AddDbContext<AppDbContext>(
    options => options.UseInMemoryDatabase("kalita_db")); // Для старта можно InMemory
// builder.Services.AddScoped<Kalita.Application.Services.EstimateService>();
// builder.Services.AddScoped<Kalita.Application.Services.ExpenseService>();
// builder.Services.AddScoped<Kalita.Application.Services.InvoiceService>();
// builder.Services.AddScoped<ContractorService>();
// builder.Services.AddScoped<DictionaryService>();



// // Подключаем WorkflowEngine, подсовываем ему путь к json-конфигу
// builder.Services.AddSingleton(
//     new Kalita.Application.Workflow.WorkflowEngine("../Kalita.Application/Workflow/Configs/estimate.workflow.json"));

// // Подключаем EstimateService (в нем будет использоваться WorkflowEngine)
// builder.Services.AddScoped<Kalita.Application.Services.EstimateService>();
// // Подключаем WorkflowEngine для Expense
// builder.Services.AddSingleton(
//     new Kalita.Application.Workflow.WorkflowEngine("../Kalita.Application/Workflow/Configs/expense.workflow.json"));
// builder.Services.AddScoped<Kalita.Application.Services.ExpenseService>();


builder.Services.AddSingleton<WorkflowEngine>(provider =>
{
    var db = provider.GetRequiredService<AppDbContext>();
    return new WorkflowEngine(db, "../Kalita.Application/Workflow/Configs/");
});

builder.Services.AddSingleton(new EntityTypeMetadataService("../Kalita.Application/Workflow/Configs/entitytypes.json"));

builder.Services.AddSingleton(new DictionaryMetadataService(
    Path.Combine(AppContext.BaseDirectory, "../Kalita.Application/Workflow/Configs/dictionarytypes.json")
));

// builder.Services.AddSingleton(new EntityMetadataService(
//     Path.Combine(AppContext.BaseDirectory, "../Kalita.Application/Workflow/Configs/entitytypes.json")
// ));

var entityMetadataService = new EntityMetadataService();
entityMetadataService.LoadFromJson("../Kalita.Application/Workflow/Configs/entitytypes.json"); // например, "../Kalita.Application/Workflow/Configs/entitytypes.json"
builder.Services.AddSingleton(entityMetadataService);


builder.Services.AddScoped<Kalita.Application.Services.WorkflowEntityService>();
builder.Services.AddScoped<DictionaryService>();
builder.Services.AddScoped<DynamicEntityService>();

builder.Services.AddEndpointsApiExplorer();
builder.Services.AddSwaggerGen();

builder.Services.AddControllers();
var app = builder.Build();

app.UseSwagger();
app.UseSwaggerUI();
builder.Services.AddSingleton<DynamicEntityService>();


// using (var scope = app.Services.CreateScope())
// {
//     var db = scope.ServiceProvider.GetRequiredService<AppDbContext>();
//     var dynamicService = new DynamicEntityService(db);

//     dynamicService.LoadEntityTypesFromJson("../Kalita.Application/Workflow/Configs/entitytypes.json");
// }



app.Use(async (context, next) =>
{
    // MVP: Чтение userId и role из заголовков
    var userId = context.Request.Headers["X-User-Id"].FirstOrDefault();
    var userRole = context.Request.Headers["X-User-Role"].FirstOrDefault();
    // Сохраним в HttpContext.Items, чтобы было доступно в контроллерах/сервисах
    context.Items["UserId"] = userId;
    context.Items["UserRole"] = userRole;
    await next();
});


app.MapControllers();
app.Run();
