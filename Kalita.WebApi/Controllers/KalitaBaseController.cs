using Microsoft.AspNetCore.Mvc;

namespace Kalita.WebApi.Controllers
{
    [ApiController]
    public abstract class KalitaBaseController : ControllerBase
    {
        // Свойства для быстрого доступа к UserId и UserRole из любого контроллера
        protected string? UserId => (string?)HttpContext.Items["UserId"];
        protected string? UserRole => (string?)HttpContext.Items["UserRole"];
        protected string? UserFio => HttpContext.Request.Headers["X-User-Fio"];
    }
}
