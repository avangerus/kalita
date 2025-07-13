using Microsoft.AspNetCore.Mvc;
using Kalita.WebApi.DTO;
using Kalita.Application.Services;
using Kalita.Domain.Entities;

namespace Kalita.WebApi.Controllers
{
    [ApiController]
    [Route("api/[controller]")]
    public class ContractorsController : KalitaBaseController
    {
        private readonly ContractorService _service;
        public ContractorsController(ContractorService service) => _service = service;

        [HttpPost]
        public IActionResult Create([FromBody] CreateContractorRequest req)
        {
            var c = _service.Create(req.Name, req.Inn, req.Kpp, req.Address);
            return Ok(c);
        }

        [HttpGet]
        public IActionResult GetAll() => Ok(_service.GetAll());

        [HttpGet("{id}")]
        public IActionResult Get(Guid id)
        {
            var c = _service.Get(id);
            if (c == null) return NotFound();
            return Ok(c);
        }

        [HttpDelete("{id}")]
        public IActionResult Delete(Guid id)
        {
            _service.Delete(id);
            return NoContent();
        }
    }
}
