// using Microsoft.AspNetCore.Mvc;
// using Kalita.Application.Services;
// using Kalita.WebApi.DTO;

// namespace Kalita.WebApi.Controllers
// {
//     [ApiController]
//     [Route("api/dictionaries")]
//     public class DictionaryController : KalitaBaseController
//     {
//         private readonly DictionaryService _service;
//         public DictionaryController(DictionaryService service) => _service = service;

//         [HttpPost("types")]
//         public IActionResult CreateType([FromBody] CreateTypeRequest req)
//         {
//             var type = _service.CreateType(req.Code, req.Name);
//             return Ok(type);
//         }

//         [HttpGet("types")]
//         public IActionResult GetTypes() => Ok(_service.GetTypes());

//         [HttpPost("{typeId}/items")]
//         public IActionResult CreateItem(Guid typeId, [FromBody] CreateItemRequest req)
//         {
//             var item = _service.CreateItem(typeId, req.Code, req.Name, req.Value, req.ParentId);
//             return Ok(item);
//         }

//         [HttpGet("{typeId}/items")]
//         public IActionResult GetItems(Guid typeId)
//         {
//             return Ok(_service.GetItems(typeId));
//         }
//     }
// }
